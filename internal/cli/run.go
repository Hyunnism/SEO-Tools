package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"seo-boost/internal/analyze"
	"seo-boost/internal/config"
	"seo-boost/internal/crawl"
	"seo-boost/internal/google"
	"seo-boost/internal/inspect"
	"seo-boost/internal/model"
	"seo-boost/internal/perf"
	"seo-boost/internal/report"
	"seo-boost/internal/sitemap"
	"seo-boost/internal/store"
)

func runOrchestrator(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	spider := true
	concurrency := 0
	maxPages := 100
	days := 28
	limit := 50
	minImp := 50
	dryRun := false
	force := false

	fs.BoolVar(&spider, "spider", true, "sertakan internal spider BFS untuk melengkapi sitemap")
	fs.IntVar(&concurrency, "concurrency", 0, "jumlah worker paralel crawling")
	fs.IntVar(&maxPages, "max-pages", 100, "maksimal total halaman yang dirayapi")
	fs.IntVar(&days, "days", 28, "rentang hari data Search Analytics GSC")
	fs.IntVar(&limit, "limit", 50, "maksimal URL yang diinspeksi ke Google")
	fs.IntVar(&minImp, "min-imp", 50, "ambang minimum impresi untuk keyword gap")
	fs.BoolVar(&dryRun, "dry-run", false, "simulasikan inspect tanpa memanggil API Google")
	fs.BoolVar(&force, "force", false, "paksa inspeksi ulang mengabaikan batas reinspect_after_days")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Muat config: %v\n", err)
		return 1
	}

	site, err := resolveSite(c, g.site)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] %v\n", err)
		return 1
	}

	log := newLogger(stderr, g.verbose)
	st := store.NewStore(c.DataDir)

	// Kunci akses eksklusif untuk site ini
	unlock, err := st.Lock(site.Name)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Mengunci store untuk site %s: %v\n", site.Name, err)
		return 1
	}
	defer unlock()

	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  SEO-BOOST PIPELINE: %s (%s)\n", site.Name, site.BaseURL)
	fmt.Fprintf(stdout, "================================================================================\n\n")

	ctx := context.Background()
	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 1: Discovery URL (Sitemap + BFS Spider)
	// -------------------------------------------------------------------------
	fmt.Fprintf(stdout, "[1/6] Menemukan URL dari Sitemap & Spider...\n")
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
		urls, smErr := sitemap.FetchAndParse(ctx, hc, sm, site.BaseURL, site.ExcludePaths, maxPages)
		if smErr != nil {
			log.Warn("gagal parse sitemap", "sitemap", sm, "error", smErr)
			continue
		}
		for _, u := range urls {
			sitemapURLsMap[u] = true
			allFoundURLsMap[u] = true
		}
	}

	if spider {
		ua := c.Defaults.UserAgent
		if ua == "" {
			ua = "seo-boost/1.0"
		}
		spiderURLs, spErr := crawl.SpiderBFS(ctx, hc, site.BaseURL, ua, maxPages)
		if spErr != nil && !errors.Is(spErr, context.Canceled) {
			log.Warn("spider BFS mengalami kendala", "error", spErr)
		}
		for _, u := range spiderURLs {
			allFoundURLsMap[u] = true
		}
	}

	var targetURLs []string
	for u := range allFoundURLsMap {
		targetURLs = append(targetURLs, u)
		if len(targetURLs) >= maxPages {
			break
		}
	}
	sort.Strings(targetURLs)

	if len(targetURLs) == 0 {
		targetURLs = append(targetURLs, site.BaseURL)
	}
	fmt.Fprintf(stdout, "      Ditemukan %d URL target untuk dirayapi.\n\n", len(targetURLs))

	// -------------------------------------------------------------------------
	// LANGKAH 2: Crawl & Audit On-Page
	// -------------------------------------------------------------------------
	crawlConc := concurrency
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

	fmt.Fprintf(stdout, "[2/6] Merayapi %d Halaman & Menjalankan Audit On-Page (concurrency=%d)...\n", len(targetURLs), crawlConc)
	crawler := crawl.NewCrawler(crawl.Config{
		UserAgent:   c.Defaults.UserAgent,
		Concurrency: crawlConc,
		TimeoutS:    crawlTimeout,
		BaseURL:     site.BaseURL,
	}, hc)

	crawledPages, crawlErr := crawler.CrawlPages(ctx, targetURLs, sitemapURLsMap)
	if crawlErr != nil {
		fmt.Fprintf(stderr, "[PERINGATAN] Crawl mengalami kendala: %v\n", crawlErr)
	}

	// Muat existing pages jika ada untuk penggabungan fail-soft
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
	fmt.Fprintf(stdout, "      Selesai merayapi %d halaman.\n\n", len(crawledPages))

	// -------------------------------------------------------------------------
	// LANGKAH 3: Google Search Console — URL Inspection
	// -------------------------------------------------------------------------
	fmt.Fprintf(stdout, "[3/6] Memeriksa Status Indeks ke Google Search Console...\n")
	var gOpts []google.ClientOption
	gOpts = append(gOpts, google.WithLogger(log))
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}

	gClient, gErr := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if gErr != nil {
		fmt.Fprintf(stdout, "      [INFO] Dilewati (Google Service Account belum dikonfigurasi / error: %v)\n\n", gErr)
	} else {
		qm := inspect.NewQuotaManager(c.DataDir, nil)
		remBudget := qm.RemainingBudget(site.Name, c.Defaults.InspectDailyBudget)

		inspectLimit := limit
		if inspectLimit > remBudget {
			inspectLimit = remBudget
		}

		reinspectDays := c.Defaults.ReinspectAfterDays
		if reinspectDays <= 0 {
			reinspectDays = 7
		}

		fullQueue := inspect.BuildInspectQueue(pages, site.PriorityPaths, reinspectDays, force, time.Now())
		var queue []inspect.PrioritizedURL
		for _, item := range fullQueue {
			queue = append(queue, item)
			if len(queue) >= inspectLimit {
				break
			}
		}

		if len(queue) == 0 {
			fmt.Fprintf(stdout, "      Tidak ada URL yang perlu diinspeksi ulang saat ini.\n\n")
		} else if dryRun {
			fmt.Fprintf(stdout, "      [DRY RUN] %d URL dijadwalkan inspeksi (sisa budget: %d).\n\n", len(queue), remBudget)
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

			inspectedCount, bErr := inspect.InspectBatch(ctx, gClient, qm, queue, workerCfg, func(item inspect.PrioritizedURL, info *model.IndexInfo, itemErr error) {
				if itemErr != nil {
					log.Debug("gagal inspect url", "url", item.URL, "error", itemErr)
					return
				}
				if p, ok := pageMap[item.URL]; ok {
					p.Index = info
				}
			})
			if bErr != nil {
				fmt.Fprintf(stderr, "      [PERINGATAN] URL Inspection error: %v\n", bErr)
			}
			fmt.Fprintf(stdout, "      Selesai menginspeksi %d URL ke GSC.\n\n", inspectedCount)
		}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 4: Google Search Console — Search Analytics (Perf)
	// -------------------------------------------------------------------------
	fmt.Fprintf(stdout, "[4/6] Mengambil Metrik Performa Pencarian GSC (%d hari)...\n", days)
	if gClient == nil {
		fmt.Fprintf(stdout, "      [INFO] Dilewati (Google Client tidak tersedia).\n\n")
	} else {
		startDate, endDate := perf.CalculateDateRange(time.Now(), days)
		agg := perf.NewAggregator(gClient, site.BaseURL)
		perfMap, pErr := agg.FetchAndAggregate(ctx, site.GSCProperty, startDate, endDate)
		if pErr != nil {
			fmt.Fprintf(stderr, "      [PERINGATAN] Search Analytics error: %v\n\n", pErr)
		} else {
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
			fmt.Fprintf(stdout, "      Berhasil mengambil data performa untuk %d halaman (%s s.d. %s).\n\n", matched, startDate, endDate)
		}
	}

	// -------------------------------------------------------------------------
	// LANGKAH 5: Analisis Keyword Gap
	// -------------------------------------------------------------------------
	fmt.Fprintf(stdout, "[5/6] Menganalisis Peluang Keyword Gap (min imp >= %d)...\n", minImp)
	totalGaps := 0
	var allGaps []model.QueryGap

	for _, p := range pages {
		p.Gaps = nil
		if p.Perf == nil || len(p.Perf.Queries) == 0 {
			continue
		}
		for _, q := range p.Perf.Queries {
			if q.Impressions < float64(minImp) {
				continue
			}
			gap := analyze.AnalyzeKeywordGap(q, p, c.Defaults.Stopwords, minImp)
			p.Gaps = append(p.Gaps, gap)
			allGaps = append(allGaps, gap)
			totalGaps++
		}
	}
	sort.Slice(allGaps, func(i, j int) bool {
		return allGaps[i].Score > allGaps[j].Score
	})
	fmt.Fprintf(stdout, "      Ditemukan %d peluang keyword gap teridentifikasi.\n\n", totalGaps)

	// -------------------------------------------------------------------------
	// LANGKAH 6: Penyimpanan Atomik & Perhitungan Diff
	// -------------------------------------------------------------------------
	fmt.Fprintf(stdout, "[6/6] Menghitung Ringkasan, Diff, dan Menyimpan ke Store...\n")

	// Pastikan pages terurut sebelum disimpan
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].URL < pages[j].URL
	})

	if err := st.SavePages(site.Name, pages); err != nil {
		fmt.Fprintf(stderr, "      [PERINGATAN] Gagal menyimpan snapshot pages: %v\n", err)
	}

	prevRun, _ := st.LoadLatestRun(site.Name)
	currSummary := store.BuildRunSummary(site.Name, pages)
	diff := report.ComputeDiff(pages, prevRun)

	if err := st.SaveRun(site.Name, currSummary); err != nil {
		fmt.Fprintf(stderr, "      [PERINGATAN] Gagal menyimpan run history: %v\n", err)
	}
	fmt.Fprintf(stdout, "      Data snapshot dan riwayat run berhasil disimpan secara atomik.\n\n")

	// -------------------------------------------------------------------------
	// LAPORAN AKHIR
	// -------------------------------------------------------------------------
	topGaps := allGaps
	if len(topGaps) > 20 {
		topGaps = topGaps[:20]
	}

	var reportedIssues []report.ReportedIssue
	for _, p := range pages {
		for _, iss := range p.Audit {
			reportedIssues = append(reportedIssues, report.ReportedIssue{
				URL:      p.URL,
				Code:     iss.Code,
				Severity: iss.Severity,
				Detail:   iss.Detail,
			})
		}
	}

	repData := &report.ReportData{
		Site:        site.Name,
		GSCProperty: site.GSCProperty,
		Timestamp:   time.Now().UTC(),
		Summary:     currSummary,
		Diff:        diff,
		TopGaps:     topGaps,
		AuditIssues: reportedIssues,
		Pages:       pages,
	}

	var outBuf bytes.Buffer
	if g.format == "json" {
		if err := report.RenderJSON(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render JSON: %v\n", err)
			return 1
		}
	} else {
		if err := report.RenderText(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render report: %v\n", err)
			return 1
		}
	}

	outputBytes := outBuf.Bytes()
	fmt.Fprint(stdout, string(outputBytes))

	if g.out != "" {
		if err := os.WriteFile(g.out, outputBytes, 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan lengkap tersimpan di: %s\n", g.out)
	}

	return 0
}
