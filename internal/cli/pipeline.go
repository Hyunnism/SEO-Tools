package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"seo-boost/internal/analyze"
	"seo-boost/internal/config"
	"seo-boost/internal/crawl"
	"seo-boost/internal/google"
	"seo-boost/internal/inspect"
	"seo-boost/internal/model"
	"seo-boost/internal/notify"
	"seo-boost/internal/perf"
	"seo-boost/internal/report"
	"seo-boost/internal/serve"
	"seo-boost/internal/sitemap"
	"seo-boost/internal/store"
)

type pipelineOptions struct {
	spider      bool
	concurrency int
	maxPages    int
	days        int
	limit       int
	minImp      int
	dryRun      bool
	force       bool
	notifyMode  notify.NotifyMode
}

func defaultPipelineOptions() pipelineOptions {
	return pipelineOptions{
		spider:      true,
		concurrency: 0,
		maxPages:    100,
		days:        28,
		limit:       50,
		minImp:      50,
		dryRun:      false,
		force:       false,
		notifyMode:  notify.NotifyAuto,
	}
}

func executePipeline(
	ctx context.Context,
	c *config.Config,
	site *config.Site,
	opts pipelineOptions,
	log *slog.Logger,
	st *store.Store,
	progress serve.ProgressCallback,
	stdout io.Writer,
) ([]*model.Page, *model.RunSummary, *report.RunDiff, []model.QueryGap, error) {
	out := func(format string, a ...any) {
		if stdout != nil {
			fmt.Fprintf(stdout, format, a...)
		}
	}
	reportProgress := func(step string, current, total int, msg string) {
		if progress != nil {
			progress(step, current, total, msg)
		}
	}

	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 1: Discovery URL (Sitemap + BFS Spider)
	// -------------------------------------------------------------------------
	out("[1/6] Menemukan URL dari Sitemap & Spider...\n")
	reportProgress("discover", 0, opts.maxPages, "Menemukan URL dari Sitemap & Spider...")

	var sitemapURLs []string
	if len(site.Sitemaps) > 0 {
		sitemapURLs = append(sitemapURLs, site.Sitemaps...)
	} else {
		discovered, dErr := sitemap.AutoDiscover(ctx, hc, site.BaseURL)
		if dErr == nil {
			sitemapURLs = append(sitemapURLs, discovered...)
		}
	}

	sitemapURLsMap := make(map[string]bool)
	allFoundURLsMap := make(map[string]bool)
	for _, sm := range sitemapURLs {
		urls, smErr := sitemap.FetchAndParse(ctx, hc, sm, site.BaseURL, site.ExcludePaths, opts.maxPages)
		if smErr != nil {
			if log != nil {
				log.Warn("gagal parse sitemap", "sitemap", sm, "error", smErr)
			}
			continue
		}
		for _, u := range urls {
			sitemapURLsMap[u] = true
			allFoundURLsMap[u] = true
		}
	}

	if opts.spider {
		ua := c.Defaults.UserAgent
		if ua == "" {
			ua = "seo-boost/1.0"
		}
		spiderURLs, spErr := crawl.SpiderBFS(ctx, hc, site.BaseURL, ua, opts.maxPages)
		if spErr != nil && !errors.Is(spErr, context.Canceled) {
			if log != nil {
				log.Warn("spider BFS mengalami kendala", "error", spErr)
			}
		}
		for _, u := range spiderURLs {
			allFoundURLsMap[u] = true
		}
	}

	var targetURLs []string
	for u := range allFoundURLsMap {
		targetURLs = append(targetURLs, u)
		if len(targetURLs) >= opts.maxPages {
			break
		}
	}
	sort.Strings(targetURLs)

	if len(targetURLs) == 0 {
		targetURLs = append(targetURLs, site.BaseURL)
	}
	out("      Ditemukan %d URL target untuk dirayapi.\n\n", len(targetURLs))
	reportProgress("discover", len(targetURLs), len(targetURLs), fmt.Sprintf("Ditemukan %d URL target.", len(targetURLs)))

	// -------------------------------------------------------------------------
	// LANGKAH 2: Crawl & Audit On-Page
	// -------------------------------------------------------------------------
	crawlConc := opts.concurrency
	if crawlConc <= 0 {
		if c.Defaults.CrawlConcurrency > 0 {
			crawlConc = c.Defaults.CrawlConcurrency
		} else {
			crawlConc = 5
		}
	}

	crawlTimeout := c.Defaults.CrawlTimeoutS
	if crawlTimeout <= 0 {
		crawlTimeout = 20
	}

	out("[2/6] Merayapi %d Halaman & Menjalankan Audit On-Page (concurrency=%d)...\n", len(targetURLs), crawlConc)
	reportProgress("crawl", 0, len(targetURLs), fmt.Sprintf("Merayapi %d halaman web...", len(targetURLs)))

	crawler := crawl.NewCrawler(crawl.Config{
		UserAgent:   c.Defaults.UserAgent,
		Concurrency: crawlConc,
		TimeoutS:    crawlTimeout,
		BaseURL:     site.BaseURL,
	}, hc)

	crawledPages, crawlErr := crawler.CrawlPages(ctx, targetURLs, sitemapURLsMap)
	if crawlErr != nil {
		out("[PERINGATAN] Crawl mengalami kendala: %v\n", crawlErr)
	}

	existingPages, _ := st.LoadPages(site.Name)
	pageMap := make(map[string]*model.Page, len(existingPages)+len(crawledPages))
	for _, ep := range existingPages {
		pageMap[ep.URL] = ep
	}
	for _, cp := range crawledPages {
		if ep, exists := pageMap[cp.URL]; exists {
			ep.Path = cp.Path
			ep.InSitemap = cp.InSitemap
			ep.InternalIn = cp.InternalIn
			ep.Crawl = cp.Crawl
			ep.Audit = cp.Audit
		} else {
			pageMap[cp.URL] = cp
		}
	}

	var pages []*model.Page
	for _, p := range pageMap {
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].URL < pages[j].URL
	})
	out("      Selesai merayapi %d halaman.\n\n", len(crawledPages))
	reportProgress("crawl", len(crawledPages), len(targetURLs), fmt.Sprintf("Selesai merayapi %d halaman.", len(crawledPages)))

	// -------------------------------------------------------------------------
	// LANGKAH 3: Google Search Console — URL Inspection
	// -------------------------------------------------------------------------
	out("[3/6] Memeriksa Status Indeks ke Google Search Console...\n")
	reportProgress("inspect", 0, 1, "Menghubungi Google Search Console...")

	var gOpts []google.ClientOption
	if log != nil {
		gOpts = append(gOpts, google.WithLogger(log))
	}
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}

	gClient, gErr := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if gErr != nil {
		out("      [INFO] Dilewati (Google Service Account belum dikonfigurasi / error: %v)\n\n", gErr)
		reportProgress("inspect", 0, 0, "Dilewati (kredensial Google SA tidak tersedia).")
	} else {
		qm := inspect.NewQuotaManager(c.DataDir, nil)
		remBudget := qm.RemainingBudget(site.Name, c.Defaults.InspectDailyBudget)

		inspectLimit := opts.limit
		if inspectLimit > remBudget {
			inspectLimit = remBudget
		}

		reinspectDays := c.Defaults.ReinspectAfterDays
		if reinspectDays <= 0 {
			reinspectDays = 7
		}

		fullQueue := inspect.BuildInspectQueue(pages, site.PriorityPaths, reinspectDays, opts.force, time.Now())
		var queue []inspect.PrioritizedURL
		for _, item := range fullQueue {
			queue = append(queue, item)
			if len(queue) >= inspectLimit {
				break
			}
		}

		if len(queue) == 0 {
			out("      Tidak ada URL yang perlu diinspeksi ulang saat ini.\n\n")
			reportProgress("inspect", 0, 0, "Tidak ada URL yang perlu diinspeksi.")
		} else if opts.dryRun {
			out("      [DRY RUN] %d URL dijadwalkan inspeksi (sisa budget: %d).\n\n", len(queue), remBudget)
			reportProgress("inspect", len(queue), len(queue), "Inspeksi GSC (dry-run).")
		} else {
			rpm := c.Defaults.InspectRPM
			if rpm <= 0 {
				rpm = 600
			}

			workerCfg := inspect.InspectWorkerConfig{
				RPM:      rpm,
				SiteName: site.Name,
				Property: site.GSCProperty,
			}

			reportProgress("inspect", 0, len(queue), fmt.Sprintf("Menginspeksi %d URL ke GSC...", len(queue)))
			inspectedCount, bErr := inspect.InspectBatch(ctx, gClient, qm, queue, workerCfg, func(item inspect.PrioritizedURL, info *model.IndexInfo, itemErr error) {
				if itemErr != nil {
					if log != nil {
						log.Debug("gagal inspect url", "url", item.URL, "error", itemErr)
					}
					return
				}
				if p, ok := pageMap[item.URL]; ok {
					p.Index = info
				}
			})
			if bErr != nil {
				out("      [PERINGATAN] URL Inspection error: %v\n", bErr)
			}
			out("      Selesai menginspeksi %d URL ke GSC.\n\n", inspectedCount)
			reportProgress("inspect", inspectedCount, len(queue), fmt.Sprintf("Selesai menginspeksi %d URL.", inspectedCount))
		}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 4: Google Search Console — Search Analytics (Perf)
	// -------------------------------------------------------------------------
	out("[4/6] Mengambil Metrik Performa Pencarian GSC (%d hari)...\n", opts.days)
	reportProgress("perf", 0, 1, "Mengambil data Search Analytics...")

	if gClient == nil {
		out("      [INFO] Dilewati (Google Client tidak tersedia).\n\n")
		reportProgress("perf", 0, 0, "Dilewati (Google Client tidak tersedia).")
	} else {
		startDate, endDate := perf.CalculateDateRange(time.Now(), opts.days)
		prevStart, prevEnd := perf.CalculatePreviousDateRange(time.Now(), opts.days)
		agg := perf.NewAggregator(gClient, site.BaseURL)
		perfMap, pErr := agg.FetchAndAggregate(ctx, site.GSCProperty, startDate, endDate)
		if pErr != nil {
			out("      [PERINGATAN] Search Analytics error: %v\n\n", pErr)
			reportProgress("perf", 0, 1, fmt.Sprintf("Search Analytics error: %v", pErr))
		} else {
			// Tarik periode pembanding sebelumnya untuk menghitung delta
			prevPerfMap, _ := agg.FetchAndAggregate(ctx, site.GSCProperty, prevStart, prevEnd)
			perf.CalculateDeltas(perfMap, prevPerfMap)

			matched := 0
			for u, pPerf := range perfMap {
				p, ok := pageMap[u]
				if !ok {
					p = &model.Page{URL: u}
					pageMap[u] = p
					pages = append(pages, p)
				}
				p.Perf = pPerf
				matched++
			}
			out("      Berhasil mengambil data performa untuk %d halaman (%s s.d. %s) & menghitung delta periode.\n\n", matched, startDate, endDate)
			reportProgress("perf", 1, 1, fmt.Sprintf("Data performa %d halaman & delta periode siap.", matched))
		}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 5: Analisis Keyword Gap
	// -------------------------------------------------------------------------
	out("[5/6] Menganalisis Peluang Keyword Gap (min imp >= %d)...\n", opts.minImp)
	reportProgress("gap", 0, len(pages), "Menganalisis keyword gap...")

	totalGaps := 0
	var allGaps []model.QueryGap

	for _, p := range pages {
		p.Gaps = nil
		if p.Perf == nil || len(p.Perf.Queries) == 0 {
			continue
		}
		for _, q := range p.Perf.Queries {
			if q.Impressions < float64(opts.minImp) {
				continue
			}
			gap := analyze.AnalyzeKeywordGap(q, p, c.Defaults.Stopwords, opts.minImp)
			p.Gaps = append(p.Gaps, gap)
			allGaps = append(allGaps, gap)
			totalGaps++
		}
	}
	sort.Slice(allGaps, func(i, j int) bool {
		return allGaps[i].Score > allGaps[j].Score
	})
	out("      Ditemukan %d peluang keyword gap teridentifikasi.\n\n", totalGaps)
	reportProgress("gap", len(pages), len(pages), fmt.Sprintf("Ditemukan %d peluang keyword gap.", totalGaps))

	// -------------------------------------------------------------------------
	// LANGKAH 6: Penyimpanan Atomik & Perhitungan Diff
	// -------------------------------------------------------------------------
	out("[6/6] Menghitung Ringkasan, Diff, dan Menyimpan ke Store...\n")
	reportProgress("store", 0, 1, "Menyimpan snapshot ke database lokal...")

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].URL < pages[j].URL
	})

	if err := st.SavePages(site.Name, pages); err != nil {
		out("      [PERINGATAN] Gagal menyimpan snapshot pages: %v\n", err)
	}

	prevRun, _ := st.LoadLatestRun(site.Name)
	currSummary := store.BuildRunSummary(site.Name, pages)
	diff := report.ComputeDiff(pages, prevRun)

	if err := st.SaveRun(site.Name, currSummary); err != nil {
		out("      [PERINGATAN] Gagal menyimpan run history: %v\n", err)
	}
	out("      Data snapshot dan riwayat run berhasil disimpan secara atomik.\n\n")

	// -------------------------------------------------------------------------
	// LANGKAH 7: Pengiriman Notifikasi (Telegram & Discord)
	// -------------------------------------------------------------------------
	hasCrit := report.HasCriticalIssues(pages, nil, site.PriorityPaths)
	notifData := &notify.NotificationData{
		Site:              site.Name,
		Timestamp:         time.Now().UTC(),
		Summary:           currSummary,
		Diff:              diff,
		HasCriticalIssues: hasCrit,
	}

	if opts.notifyMode != "" && opts.notifyMode != notify.NotifyOff {
		sent, nErr := notify.Dispatch(ctx, hc, c.Notify, opts.notifyMode, notifData)
		if nErr != nil && !errors.Is(nErr, notify.ErrNoChannelsConfigured) {
			out("      [PERINGATAN] Gagal mengirim notifikasi: %v\n", nErr)
		} else if sent {
			out("      [INFO] Notifikasi ringkasan & diff berhasil dikirim.\n\n")
		}
	}

	reportProgress("done", len(pages), len(pages), "Pipeline selesai dengan sukses.")

	return pages, currSummary, diff, allGaps, nil
}
