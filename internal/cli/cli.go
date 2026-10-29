package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
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

const usage = `seo-boost - SEO automation tool

Pemakaian: seo-boost <perintah> [flag]

Perintah tersedia:
  version   tampilkan versi
  doctor    periksa konfigurasi, kredensial, dan izin properti GSC
  discover  temukan URL situs dari sitemap dan spider BFS internal
  crawl     rayapi halaman web dan audit on-page SEO
  inspect   periksa status crawl & index halaman dari Google Search Console
  perf      tarik metrik performa pencarian (klik, impresi, posisi) dari GSC
  gap       analisis peluang keyword gap antara Search Analytics dan on-page
  report    tampilkan laporan SEO komprehensif beserta perbandingan (diff)
  run       eksekusi pipeline penuh: discover -> crawl -> inspect -> perf -> gap -> report
  submit    kirim sitemap ke GSC, ping IndexNow, dan Google Indexing API
  serve     jalankan dashboard web lokal terpadu (UI & REST API)
  notify    kirim notifikasi ringkas & diff ke Telegram / Discord
  indexnow-key buat kunci autentikasi baru untuk protokol IndexNow
  help      tampilkan bantuan ini

Flag global: --config --site --format --out --verbose
`

type global struct {
	config, site, format, out string
	verbose                   bool
}

func globalFlags(fs *flag.FlagSet) *global {
	g := &global{}
	fs.StringVar(&g.config, "config", "./config.json", "path ke config.json")
	fs.StringVar(&g.site, "site", "", `nama site di config ("all" = semua)`)
	fs.StringVar(&g.format, "format", "text", "text | json | md | html")
	fs.StringVar(&g.out, "out", "", "tulis laporan ke file")
	fs.BoolVar(&g.verbose, "verbose", false, "log detail")
	return g
}

func newLogger(w io.Writer, verbose bool) *slog.Logger {
	lvl := slog.LevelInfo
	if verbose {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}

// defaultHTTPClient dapat dioverride di test
var defaultHTTPClient *http.Client

// Run mengembalikan exit code: 0 sukses, 1 error runtime/penggunaan.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "doctor":
		return doctor(rest, stdout, stderr)
	case "discover":
		return discover(rest, stdout, stderr)
	case "crawl":
		return crawlCmd(rest, stdout, stderr)
	case "inspect":
		return inspectCmd(rest, stdout, stderr)
	case "perf":
		return perfCmd(rest, stdout, stderr)
	case "gap":
		return gapCmd(rest, stdout, stderr)
	case "report":
		return reportCmd(rest, stdout, stderr)
	case "run":
		return runOrchestrator(rest, stdout, stderr)
	case "submit":
		return submitCmd(rest, stdout, stderr)
	case "indexnow-key":
		return indexnowKeyCmd(rest, stdout, stderr)
	case "serve":
		return serveCmd(rest, stdout, stderr)
	case "notify":
		return notifyCmd(rest, stdout, stderr)
	case "log-server":
		fmt.Fprintf(stderr, "perintah %q belum tersedia\n", cmd)
		return 1
	default:
		fmt.Fprintf(stderr, "perintah tidak dikenal: %q\n\n%s", cmd, usage)
		return 1
	}
}

func doctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	configOnly := false
	fs.BoolVar(&configOnly, "config-only", false, "hanya validasi config tanpa menghubungi Google API")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)

	// 1. Periksa versi Go
	goVer := runtime.Version()
	log.Debug("memeriksa versi Go runtime", "version", goVer)
	if err := checkGoVersion(goVer); err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Versi Go: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Versi Go: %s (memenuhi syarat >= 1.22)\n", goVer)

	// 2. Load & validasi file config
	log.Debug("memuat config", "path", g.config)
	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintln(stderr, "[GAGAL] Validasi Config:")
		fmt.Fprintln(stderr, strings.TrimSpace(err.Error()))
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Config terstruktur dengan benar (%d site terdaftar)\n", len(c.Sites))

	if configOnly {
		return 0
	}

	// 3. Inisialisasi Google Client menggunakan Service Account
	var gOpts []google.ClientOption
	gOpts = append(gOpts, google.WithLogger(log))
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}

	gClient, err := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Service Account Key: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "[OK] Service Account Key valid (PKCS#8 RSA)")

	// 4. Periksa akses ke Google Search Console (sites.list)
	ctx := context.Background()
	log.Debug("mengambil daftar properti situs dari GSC...")
	remoteSites, err := gClient.ListSites(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Akses GSC API: %v\n", err)
		fmt.Fprintln(stderr, "  -> Pastikan 'Google Search Console API' sudah di-ENABLE di Google Cloud Console")
		fmt.Fprintln(stderr, "  -> Pastikan akun Service Account sudah ditambahkan di GSC > Settings > Users & Permissions")
		return 1
	}
	fmt.Fprintf(stdout, "[OK] Berhasil terhubung ke GSC API (%d properti terdaftar pada Service Account)\n", len(remoteSites))

	// Peta properti terdaftar di GSC: siteUrl -> permissionLevel
	remoteMap := make(map[string]string)
	for _, s := range remoteSites {
		remoteMap[s.SiteURL] = s.PermissionLevel
	}

	// 5. Verifikasi izin tiap site di config
	hasError := false
	for _, site := range c.Sites {
		perm, found := remoteMap[site.GSCProperty]
		if !found {
			fmt.Fprintf(stderr, "[GAGAL] Site %q (%s): tidak ditemukan di daftar properti GSC\n", site.Name, site.GSCProperty)
			fmt.Fprintln(stderr, "  -> Periksa apakah format gsc_property sudah sama persis (mis. sc-domain:example.com atau https://example.com/ dengan trailing slash)")
			fmt.Fprintln(stderr, "  -> Pastikan email Service Account sudah diundang di properti ini pada Search Console")
			hasError = true
			continue
		}

		permDisplay := perm
		switch perm {
		case "siteOwner":
			permDisplay = "Owner (Akses Penuh)"
		case "siteFullUser":
			permDisplay = "Full (Dapat submit sitemap & inspect)"
		case "siteRestrictedUser":
			permDisplay = "Restricted (Hanya baca/inspect)"
		}
		fmt.Fprintf(stdout, "[OK] Site %q (%s) -> Izin: %s\n", site.Name, site.GSCProperty, permDisplay)
	}

	if hasError {
		fmt.Fprintln(stderr, "\nDaftar properti yang saat ini terdaftar pada akun Service Account:")
		if len(remoteSites) == 0 {
			fmt.Fprintln(stderr, "  (Belum ada properti apapun. Undang Service Account di Search Console Settings > Users)")
		} else {
			for _, s := range remoteSites {
				fmt.Fprintf(stderr, "  - %s (Izin: %s)\n", s.SiteURL, s.PermissionLevel)
			}
		}
		return 1
	}

	fmt.Fprintln(stdout, "\nSemua pemeriksaan doctor sukses! Tool siap digunakan.")
	return 0
}

func checkGoVersion(ver string) error {
	v := strings.TrimPrefix(ver, "go")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		// Jika versi devel atau non-standar, lewati pengecekan ketat
		return nil
	}

	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}

	if major < 1 || (major == 1 && minor < 22) {
		return fmt.Errorf("versi Go saat ini adalah %s, minimal membutuhkan Go 1.22+", ver)
	}
	return nil
}

func resolveSite(cfg *config.Config, siteName string) (*config.Site, error) {
	if len(cfg.Sites) == 0 {
		return nil, errors.New("tidak ada site terdaftar di config")
	}
	if siteName == "" {
		if len(cfg.Sites) == 1 {
			return &cfg.Sites[0], nil
		}
		var names []string
		for _, s := range cfg.Sites {
			names = append(names, s.Name)
		}
		return nil, fmt.Errorf("parameter --site harus ditentukan karena ada lebih dari 1 site: [%s]", strings.Join(names, ", "))
	}
	for i := range cfg.Sites {
		if cfg.Sites[i].Name == siteName {
			return &cfg.Sites[i], nil
		}
	}
	return nil, fmt.Errorf("site %q tidak ditemukan di config", siteName)
}

func discover(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	spider := false
	maxPages := 100
	fs.BoolVar(&spider, "spider", false, "jalankan spider internal BFS untuk mendeteksi orphan/unlinked URL")
	fs.IntVar(&maxPages, "max-pages", 100, "batas maksimal halaman yang dikumpulkan")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)
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

	ctx := context.Background()
	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	// 1. Tentukan URL sitemap (dari config atau auto-discover)
	var sitemapURLs []string
	if len(site.Sitemaps) > 0 {
		sitemapURLs = append(sitemapURLs, site.Sitemaps...)
	} else {
		log.Debug("mencari sitemap otomatis dari robots.txt / fallback", "baseURL", site.BaseURL)
		discoveredSitemaps, err := sitemap.AutoDiscover(ctx, hc, site.BaseURL)
		if err != nil {
			log.Debug("auto-discover sitemap tidak menemukan hasil", "err", err)
		} else {
			sitemapURLs = append(sitemapURLs, discoveredSitemaps...)
		}
	}

	// 2. Fetch URLs dari sitemap
	sitemapURLsMap := make(map[string]bool)
	var sitemapList []string
	for _, sm := range sitemapURLs {
		urls, err := sitemap.FetchAndParse(ctx, hc, sm, site.BaseURL, site.ExcludePaths, maxPages)
		if err != nil {
			fmt.Fprintf(stderr, "[PERINGATAN] Gagal parse sitemap %s: %v\n", sm, err)
			continue
		}
		for _, u := range urls {
			if !sitemapURLsMap[u] {
				sitemapURLsMap[u] = true
				sitemapList = append(sitemapList, u)
			}
		}
	}

	// 3. Jika --spider aktif, jalankan spider BFS
	spiderURLsMap := make(map[string]bool)
	var spiderList []string
	if spider {
		log.Debug("menjalankan internal spider BFS...", "baseURL", site.BaseURL, "maxPages", maxPages)
		spURLs, err := crawl.SpiderBFS(ctx, hc, site.BaseURL, "seo-boost/1.0", maxPages)
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(stderr, "[PERINGATAN] Spider gagal: %v\n", err)
		}
		for _, u := range spURLs {
			if !spiderURLsMap[u] {
				spiderURLsMap[u] = true
				spiderList = append(spiderList, u)
			}
		}
	}

	// Gabungkan dan hitung statistik
	allURLsMap := make(map[string]bool)
	for u := range sitemapURLsMap {
		allURLsMap[u] = true
	}
	for u := range spiderURLsMap {
		allURLsMap[u] = true
	}

	var allURLs []string
	for u := range allURLsMap {
		allURLs = append(allURLs, u)
	}
	sort.Strings(allURLs)

	// Format output
	var outBuf bytes.Buffer
	fmt.Fprintf(&outBuf, "=== Discovery Report untuk %s (%s) ===\n", site.Name, site.BaseURL)
	fmt.Fprintf(&outBuf, "Sitemap URLs: %d\n", len(sitemapList))
	if spider {
		fmt.Fprintf(&outBuf, "Spider URLs:  %d\n", len(spiderList))
		var orphanInSpider []string
		for _, u := range spiderList {
			if !sitemapURLsMap[u] {
				orphanInSpider = append(orphanInSpider, u)
			}
		}
		var orphanInSitemap []string
		for _, u := range sitemapList {
			if !spiderURLsMap[u] {
				orphanInSitemap = append(orphanInSitemap, u)
			}
		}
		fmt.Fprintf(&outBuf, "URLs di spider tapi TIDAK di sitemap: %d\n", len(orphanInSpider))
		fmt.Fprintf(&outBuf, "URLs di sitemap tapi TIDAK terhubung link internal: %d\n", len(orphanInSitemap))
	}
	fmt.Fprintf(&outBuf, "Total Unik URLs: %d\n\n", len(allURLs))

	for i, u := range allURLs {
		tag := "[SITEMAP]"
		if spider {
			inSM := sitemapURLsMap[u]
			inSP := spiderURLsMap[u]
			if inSM && inSP {
				tag = "[BOTH]   "
			} else if inSM {
				tag = "[SITEMAP]"
			} else {
				tag = "[SPIDER] "
			}
		}
		fmt.Fprintf(&outBuf, "%3d. %s %s\n", i+1, tag, u)
	}

	resultStr := outBuf.String()
	fmt.Fprint(stdout, resultStr)

	if g.out != "" {
		if err := os.WriteFile(g.out, []byte(resultStr), 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan tersimpan di: %s\n", g.out)
	}

	return 0
}

func crawlCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crawl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	spider := true
	concurrency := 0
	maxPages := 100
	timeoutSec := 20
	fs.BoolVar(&spider, "spider", true, "sertakan internal spider BFS untuk melengkapi sitemap")
	fs.IntVar(&concurrency, "concurrency", 0, "jumlah worker paralel (default mengikuti config atau 5)")
	fs.IntVar(&maxPages, "max-pages", 100, "maksimal halaman yang dirayapi")
	fs.IntVar(&timeoutSec, "timeout", 20, "timeout HTTP request per halaman dalam detik")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)
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

	if concurrency <= 0 {
		if c.Defaults.CrawlConcurrency > 0 {
			concurrency = c.Defaults.CrawlConcurrency
		} else {
			concurrency = 5
		}
	}

	ctx := context.Background()
	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	}

	// 1. Kumpulkan daftar URL (dari sitemap + spider opsional)
	sitemapURLsMap := make(map[string]bool)
	allURLsMap := make(map[string]bool)

	var sitemaps []string
	if len(site.Sitemaps) > 0 {
		sitemaps = append(sitemaps, site.Sitemaps...)
	} else {
		discovered, _ := sitemap.AutoDiscover(ctx, hc, site.BaseURL)
		sitemaps = append(sitemaps, discovered...)
	}

	for _, sm := range sitemaps {
		urls, err := sitemap.FetchAndParse(ctx, hc, sm, site.BaseURL, site.ExcludePaths, maxPages)
		if err == nil {
			for _, u := range urls {
				sitemapURLsMap[u] = true
				allURLsMap[u] = true
			}
		}
	}

	if spider || len(allURLsMap) == 0 {
		spURLs, err := crawl.SpiderBFS(ctx, hc, site.BaseURL, "seo-boost/1.0", maxPages)
		if err == nil {
			for _, u := range spURLs {
				allURLsMap[u] = true
			}
		}
	}

	var targetURLs []string
	for u := range allURLsMap {
		targetURLs = append(targetURLs, u)
		if maxPages > 0 && len(targetURLs) >= maxPages {
			break
		}
	}
	sort.Strings(targetURLs)

	if len(targetURLs) == 0 {
		fmt.Fprintf(stderr, "[PERINGATAN] Tidak ada URL yang ditemukan untuk dirayapi pada site %s\n", site.Name)
		return 1
	}

	fmt.Fprintf(stdout, "Memulai crawling %d halaman pada %s (concurrency=%d)...\n", len(targetURLs), site.BaseURL, concurrency)

	crawler := crawl.NewCrawler(crawl.Config{
		UserAgent:   "seo-boost/1.0",
		Concurrency: concurrency,
		TimeoutS:    timeoutSec,
		BaseURL:     site.BaseURL,
	}, hc)

	pages, err := crawler.CrawlPages(ctx, targetURLs, sitemapURLsMap)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Crawling error: %v\n", err)
		return 1
	}

	// Ringkasan hasil crawl
	var outBuf bytes.Buffer
	fmt.Fprintf(&outBuf, "\n=== Hasil Crawl & On-Page Audit (%s) ===\n", site.Name)
	fmt.Fprintf(&outBuf, "Total Halaman Dirayapi: %d\n", len(pages))

	statusCounts := make(map[int]int)
	totalIssues := 0
	critCount := 0
	warnCount := 0
	infoCount := 0

	for _, p := range pages {
		if p.Crawl != nil {
			statusCounts[p.Crawl.HTTPStatus]++
		}
		for _, issue := range p.Audit {
			totalIssues++
			switch issue.Severity {
			case "error":
				critCount++
			case "warn":
				warnCount++
			case "info":
				infoCount++
			}
		}
	}

	fmt.Fprintf(&outBuf, "Status Breakdown: ")
	for st, count := range statusCounts {
		fmt.Fprintf(&outBuf, "%d: %d  ", st, count)
	}
	fmt.Fprintf(&outBuf, "\nTotal Isu Audit: %d (Error: %d, Warning: %d, Info: %d)\n\n",
		totalIssues, critCount, warnCount, infoCount)

	for i, p := range pages {
		st := 0
		title := "(tanpa title)"
		words := 0
		if p.Crawl != nil {
			st = p.Crawl.HTTPStatus
			if p.Crawl.Title != "" {
				title = p.Crawl.Title
			}
			words = p.Crawl.WordCount
		}

		fmt.Fprintf(&outBuf, "%2d. [%d] %s (words: %d, links in: %d)\n    URL: %s\n",
			i+1, st, title, words, p.InternalIn, p.URL)

		if len(p.Audit) > 0 {
			for _, iss := range p.Audit {
				fmt.Fprintf(&outBuf, "    - [%s] %s: %s\n", strings.ToUpper(iss.Severity), iss.Code, iss.Detail)
			}
		}
	}

	resultStr := outBuf.String()
	fmt.Fprint(stdout, resultStr)

	// Simpan snapshot crawl & audit ke store
	st := store.NewStore(c.DataDir)
	existingPages, _ := st.LoadPages(site.Name)
	pageMap := make(map[string]*model.Page)
	for _, ep := range existingPages {
		pageMap[ep.URL] = ep
	}
	for _, p := range pages {
		if ep, ok := pageMap[p.URL]; ok {
			ep.Path = p.Path
			ep.InSitemap = p.InSitemap
			ep.InternalIn = p.InternalIn
			ep.Crawl = p.Crawl
			ep.Audit = p.Audit
		} else {
			pageMap[p.URL] = p
		}
	}
	var merged []*model.Page
	for _, p := range pageMap {
		merged = append(merged, p)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].URL < merged[j].URL
	})
	_ = st.SavePages(site.Name, merged)

	if g.out != "" {
		if err := os.WriteFile(g.out, []byte(resultStr), 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan tersimpan di: %s\n", g.out)
	}

	_ = log
	return 0
}

func inspectCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	limit := 50
	force := false
	dryRun := false
	fs.IntVar(&limit, "limit", 50, "batas maksimal URL yang diinspeksi pada run ini")
	fs.BoolVar(&force, "force", false, "inspeksi ulang meskipun usia inspeksi belum lewat reinspect_after_days")
	fs.BoolVar(&dryRun, "dry-run", false, "hanya tampilkan antrian dan sisa kuota tanpa memanggil Google API")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)
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

	ctx := context.Background()
	hc := defaultHTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	// 1. Kumpulkan URL yang ada (dari sitemap)
	var sitemaps []string
	if len(site.Sitemaps) > 0 {
		sitemaps = append(sitemaps, site.Sitemaps...)
	} else {
		discovered, _ := sitemap.AutoDiscover(ctx, hc, site.BaseURL)
		sitemaps = append(sitemaps, discovered...)
	}

	var pages []*model.Page
	seenURLs := make(map[string]bool)

	for _, sm := range sitemaps {
		urls, err := sitemap.FetchAndParse(ctx, hc, sm, site.BaseURL, site.ExcludePaths, 0)
		if err == nil {
			for _, u := range urls {
				if !seenURLs[u] {
					seenURLs[u] = true
					pages = append(pages, &model.Page{
						URL:       u,
						InSitemap: true,
					})
				}
			}
		}
	}

	if len(pages) == 0 {
		pages = append(pages, &model.Page{
			URL:       site.BaseURL,
			InSitemap: false,
		})
	}

	// 2. Siapkan QuotaManager
	qm := inspect.NewQuotaManager(c.DataDir, nil)
	usedToday, _ := qm.GetUsedToday(site.Name)
	remBudget := qm.RemainingBudget(site.Name, c.Defaults.InspectDailyBudget)

	// 3. Susun antrian terprioritas
	queue := inspect.BuildInspectQueue(pages, site.PriorityPaths, c.Defaults.ReinspectAfterDays, force, time.Now())

	fmt.Fprintf(stdout, "=== GSC URL Inspection (%s) ===\n", site.Name)
	fmt.Fprintf(stdout, "Target Properti: %s\n", site.GSCProperty)
	fmt.Fprintf(stdout, "Kuota Hari Ini (Pacific Time): %d terpakai, %d sisa budget (Budget Harian: %d)\n",
		usedToday, remBudget, c.Defaults.InspectDailyBudget)
	fmt.Fprintf(stdout, "Total URL Siap Inspeksi: %d kandidat\n", len(queue))

	if dryRun {
		fmt.Fprintln(stdout, "\n[DRY RUN] Daftar antrian prioritas inspeksi:")
		maxShow := limit
		if maxShow > len(queue) {
			maxShow = len(queue)
		}
		for i := 0; i < maxShow; i++ {
			item := queue[i]
			fmt.Fprintf(stdout, " %2d. [P%d: %s] %s\n", i+1, item.Priority, item.Reason, item.URL)
		}
		if len(queue) > maxShow {
			fmt.Fprintf(stdout, " ...dan %d URL lainnya.\n", len(queue)-maxShow)
		}
		return 0
	}

	if remBudget <= 0 {
		fmt.Fprintln(stderr, "\n[PERINGATAN] Budget inspeksi harian untuk site ini sudah habis. Gunakan besok atau naikkan inspect_daily_budget di config.json.")
		return 0
	}

	effectiveLimit := limit
	if remBudget < effectiveLimit {
		effectiveLimit = remBudget
	}
	if len(queue) > effectiveLimit {
		queue = queue[:effectiveLimit]
	}

	if len(queue) == 0 {
		fmt.Fprintln(stdout, "\nTidak ada URL yang perlu diinspeksi saat ini. Gunakan flag --force untuk memaksa inspeksi ulang.")
		return 0
	}

	// Inisialisasi Google Client
	var gOpts []google.ClientOption
	gOpts = append(gOpts, google.WithLogger(log))
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}
	gClient, err := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Inisialisasi Google Client: %v\n", err)
		return 1
	}

	rpm := c.Defaults.InspectRPM
	if rpm <= 0 {
		rpm = 600
	}
	fmt.Fprintf(stdout, "\nMemulai inspeksi %d URL (Rate limit: %d RPM)...\n\n", len(queue), rpm)

	var outBuf bytes.Buffer
	statusCounts := make(map[model.IndexStatus]int)
	inspectedMap := make(map[string]*model.IndexInfo)

	workerCfg := inspect.InspectWorkerConfig{
		RPM:      rpm,
		SiteName: site.Name,
		Property: site.GSCProperty,
	}

	processed, batchErr := inspect.InspectBatch(ctx, gClient, qm, queue, workerCfg, func(item inspect.PrioritizedURL, info *model.IndexInfo, itemErr error) {
		if itemErr != nil {
			fmt.Fprintf(stdout, "[-] [GAGAL] %s -> %v\n", item.URL, itemErr)
			return
		}
		statusCounts[info.Status]++
		inspectedMap[item.URL] = info

		canonInfo := ""
		if info.GoogleCanon != "" {
			canonInfo = fmt.Sprintf(" (Canon: %s)", info.GoogleCanon)
		}
		fmt.Fprintf(stdout, "[+] [%s] %s -> %s%s\n", strings.ToUpper(info.Verdict), item.URL, info.Status, canonInfo)
		fmt.Fprintf(&outBuf, "- [%s] %s -> Status: %s, Coverage: %q, PageFetch: %s%s\n",
			strings.ToUpper(info.Verdict), item.URL, info.Status, info.RawCoverage, info.PageFetch, canonInfo)
	})

	fmt.Fprintf(stdout, "\nSelesai menginspeksi %d URL.\nRingkasan Status: ", processed)
	for st, count := range statusCounts {
		fmt.Fprintf(stdout, "%s: %d  ", st, count)
	}
	fmt.Fprintln(stdout)

	// Simpan snapshot status indeks ke store
	if len(inspectedMap) > 0 {
		st := store.NewStore(c.DataDir)
		pages, _ := st.LoadPages(site.Name)
		pageMap := make(map[string]*model.Page)
		for _, p := range pages {
			pageMap[p.URL] = p
		}
		for u, info := range inspectedMap {
			p, ok := pageMap[u]
			if !ok {
				p = &model.Page{URL: u}
				pageMap[u] = p
			}
			p.Index = info
		}
		var updatedPages []*model.Page
		for _, p := range pageMap {
			updatedPages = append(updatedPages, p)
		}
		sort.Slice(updatedPages, func(i, j int) bool {
			return updatedPages[i].URL < updatedPages[j].URL
		})
		_ = st.SavePages(site.Name, updatedPages)
	}

	if batchErr != nil {
		fmt.Fprintf(stderr, "\n[PERINGATAN] %v\n", batchErr)
	}

	if g.out != "" {
		summaryStr := fmt.Sprintf("=== Laporan URL Inspection (%s) ===\nTanggal: %s\nTotal: %d\n\n%s",
			site.Name, time.Now().Format("2006-01-02 15:04:05"), processed, outBuf.String())
		if err := os.WriteFile(g.out, []byte(summaryStr), 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "Laporan inspeksi disimpan di: %s\n", g.out)
	}

	return 0
}

func perfCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	days := 28
	fs.IntVar(&days, "days", 28, "jumlah hari rentang data performa yang ditarik")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	log := newLogger(stderr, g.verbose)
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

	if days <= 0 {
		if c.Defaults.PerfDays > 0 {
			days = c.Defaults.PerfDays
		} else {
			days = 28
		}
	}

	startDate, endDate := perf.CalculateDateRange(time.Now(), days)

	var gOpts []google.ClientOption
	gOpts = append(gOpts, google.WithLogger(log))
	if defaultHTTPClient != nil {
		gOpts = append(gOpts, google.WithHTTPClient(defaultHTTPClient))
	}
	gClient, err := google.NewClientFromFile(c.ServiceAccountFile, gOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Inisialisasi Google Client: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Mengambil data performa GSC untuk %s (%s s.d. %s)...\n", site.Name, startDate, endDate)

	ctx := context.Background()
	agg := perf.NewAggregator(gClient, site.BaseURL)
	perfMap, err := agg.FetchAndAggregate(ctx, site.GSCProperty, startDate, endDate)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Ambil data Search Analytics: %v\n", err)
		return 1
	}

	var pageURLs []string
	totalClicks := 0.0
	totalImpressions := 0.0
	weightedPosSum := 0.0

	for u, p := range perfMap {
		pageURLs = append(pageURLs, u)
		totalClicks += p.Clicks
		totalImpressions += p.Impressions
		weightedPosSum += p.Position * p.Impressions
	}
	sort.Strings(pageURLs)

	avgPosition := 0.0
	overallCTR := 0.0
	if totalImpressions > 0 {
		avgPosition = weightedPosSum / totalImpressions
		overallCTR = totalClicks / totalImpressions
	}

	var outBuf bytes.Buffer
	fmt.Fprintf(&outBuf, "\n=== Performa Pencarian GSC (%s) ===\n", site.Name)
	fmt.Fprintf(&outBuf, "Periode: %s s.d. %s (%d hari)\n", startDate, endDate, days)
	fmt.Fprintf(&outBuf, "Total Halaman Tayang: %d\n", len(pageURLs))
	fmt.Fprintf(&outBuf, "Total Klik: %.0f | Total Impresi: %.0f | Rata-rata CTR: %.2f%% | Rata-rata Posisi: %.1f\n\n",
		totalClicks, totalImpressions, overallCTR*100, avgPosition)

	if len(pageURLs) == 0 {
		fmt.Fprintln(&outBuf, "(Belum ada data impresi atau klik tercatat pada periode ini)")
	} else {
		for i, u := range pageURLs {
			p := perfMap[u]
			badge := "    "
			if p.Page1 {
				badge = "[P1]"
			}
			fmt.Fprintf(&outBuf, "%2d. %s %s\n    Klik: %.0f | Impresi: %.0f | CTR: %.2f%% | Pos: %.1f\n",
				i+1, badge, u, p.Clicks, p.Impressions, p.CTR*100, p.Position)

			if len(p.Queries) > 0 {
				maxQ := 5
				if len(p.Queries) < maxQ {
					maxQ = len(p.Queries)
				}
				var qList []string
				for _, q := range p.Queries[:maxQ] {
					qList = append(qList, fmt.Sprintf("%q (%.0f klk, %.0f imp, pos %.1f)", q.Query, q.Clicks, q.Impressions, q.Position))
				}
				fmt.Fprintf(&outBuf, "    Top Kueri: %s\n", strings.Join(qList, "; "))
			}
		}
	}

	resultStr := outBuf.String()
	fmt.Fprint(stdout, resultStr)

	// Simpan snapshot performa ke store
	st := store.NewStore(c.DataDir)
	pages, _ := st.LoadPages(site.Name)
	pageMap := make(map[string]*model.Page)
	for _, p := range pages {
		pageMap[p.URL] = p
	}
	for u, pPerf := range perfMap {
		p, ok := pageMap[u]
		if !ok {
			p = &model.Page{URL: u}
			pageMap[u] = p
		}
		p.Perf = pPerf
	}
	var updatedPages []*model.Page
	for _, p := range pageMap {
		updatedPages = append(updatedPages, p)
	}
	sort.Slice(updatedPages, func(i, j int) bool {
		return updatedPages[i].URL < updatedPages[j].URL
	})
	_ = st.SavePages(site.Name, updatedPages)

	if g.out != "" {
		if err := os.WriteFile(g.out, []byte(resultStr), 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan performa tersimpan di: %s\n", g.out)
	}

	return 0
}

func gapCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	minImp := 50
	fs.IntVar(&minImp, "min-imp", 50, "ambang minimum impresi untuk dianalisis gap")

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

	st := store.NewStore(c.DataDir)
	pages, err := st.LoadPages(site.Name)
	if err != nil || len(pages) == 0 {
		fmt.Fprintf(stderr, "[GAGAL] Data halaman belum tersedia untuk %s. Silakan jalankan 'crawl' dan 'perf' terlebih dahulu.\n", site.Name)
		return 1
	}

	type GapDisplayItem struct {
		model.QueryGap
		URL string `json:"url"`
	}

	var allGaps []GapDisplayItem
	analyzedQueries := 0

	for _, p := range pages {
		p.Gaps = nil
		if p.Perf == nil || len(p.Perf.Queries) == 0 {
			continue
		}

		for _, q := range p.Perf.Queries {
			if q.Impressions < float64(minImp) {
				continue
			}
			analyzedQueries++
			gap := analyze.AnalyzeKeywordGap(q, p, c.Defaults.Stopwords, minImp)
			p.Gaps = append(p.Gaps, gap)
			allGaps = append(allGaps, GapDisplayItem{
				QueryGap: gap,
				URL:      p.URL,
			})
		}
	}

	// Urutkan peluang berdasarkan Opportunity Score descending
	sort.Slice(allGaps, func(i, j int) bool {
		return allGaps[i].Score > allGaps[j].Score
	})

	// Simpan gaps kembali ke store
	_ = st.SavePages(site.Name, pages)

	if g.format == "json" {
		envelope := struct {
			Schema         int              `json:"schema"`
			Site           string           `json:"site"`
			Timestamp      time.Time        `json:"timestamp"`
			MinImpressions int              `json:"min_impressions"`
			TotalGaps      int              `json:"total_gaps"`
			Gaps           []GapDisplayItem `json:"gaps"`
		}{
			Schema:         1,
			Site:           site.Name,
			Timestamp:      time.Now().UTC(),
			MinImpressions: minImp,
			TotalGaps:      len(allGaps),
			Gaps:           allGaps,
		}

		b, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Marshal JSON: %v\n", err)
			return 1
		}

		if g.out != "" {
			if err := os.WriteFile(g.out, b, 0o644); err != nil {
				fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
				return 1
			}
			fmt.Fprintf(stdout, "Laporan gap JSON tersimpan di: %s\n", g.out)
		} else {
			fmt.Fprintln(stdout, string(b))
		}
		return 0
	}

	// Format text
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "=== ANALISIS KEYWORD GAP (%s) ===\n", site.Name)
	fmt.Fprintf(&buf, "Ambang Min Impresi: %d | Kueri Dianalisis: %d | Total Peluang: %d\n\n",
		minImp, analyzedQueries, len(allGaps))

	if len(allGaps) == 0 {
		fmt.Fprintf(&buf, "Tidak ditemukan peluang keyword gap dengan impresi >= %d.\n", minImp)
	} else {
		tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Kategori\tKata Kunci\tPosisi\tImp\tCTR\tMatch\tSkor\tURL\n")
		for _, item := range allGaps {
			badge := fmt.Sprintf("[%s]", item.Category)
			fmt.Fprintf(tw, "%s\t%s\t%.1f\t%.0f\t%.1f%%\t%s\t%.1f\t%s\n",
				badge,
				item.Query,
				item.Position,
				item.Impressions,
				item.CTR*100,
				item.Match,
				item.Score,
				item.URL,
			)
		}
		tw.Flush()
	}

	resultText := buf.String()
	fmt.Fprint(stdout, resultText)

	if g.out != "" {
		if err := os.WriteFile(g.out, []byte(resultText), 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan gap tersimpan di: %s\n", g.out)
	}

	return 0
}

func reportCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	failOnIssues := false
	diffOnly := false
	fs.BoolVar(&failOnIssues, "fail-on-issues", false, "kembalikan exit code 2 bila ditemukan masalah kritis")
	fs.BoolVar(&diffOnly, "diff", false, "tampilkan perbandingan terhadap run sebelumnya")

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

	st := store.NewStore(c.DataDir)
	pages, err := st.LoadPages(site.Name)
	if err != nil || len(pages) == 0 {
		fmt.Fprintf(stderr, "[GAGAL] Data halaman belum tersedia untuk %s. Silakan jalankan 'crawl', 'inspect', atau 'perf' terlebih dahulu.\n", site.Name)
		return 1
	}

	prevRun, _ := st.LoadLatestRun(site.Name)
	currSummary := store.BuildRunSummary(site.Name, pages)
	diff := report.ComputeDiff(pages, prevRun)

	// Kumpulkan Top Gaps (maksimal 20)
	var allGaps []model.QueryGap
	for _, p := range pages {
		allGaps = append(allGaps, p.Gaps...)
	}
	sort.Slice(allGaps, func(i, j int) bool {
		return allGaps[i].Score > allGaps[j].Score
	})
	topGaps := allGaps
	if len(topGaps) > 20 {
		topGaps = topGaps[:20]
	}

	// Kumpulkan semua Audit Issues
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
	switch strings.ToLower(g.format) {
	case "json":
		if err := report.RenderJSON(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render JSON: %v\n", err)
			return 1
		}
	case "md", "markdown":
		if err := report.RenderMarkdown(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render Markdown: %v\n", err)
			return 1
		}
	case "html":
		if err := report.RenderHTML(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render HTML: %v\n", err)
			return 1
		}
	default:
		if err := report.RenderText(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render report: %v\n", err)
			return 1
		}
	}

	outputBytes := outBuf.Bytes()
	fmt.Fprint(stdout, string(outputBytes))

	// Simpan snapshot run summary ke history runs
	if err := st.SaveRun(site.Name, currSummary); err != nil {
		fmt.Fprintf(stderr, "[PERINGATAN] Gagal menyimpan run history: %v\n", err)
	}

	if g.out != "" {
		if err := os.WriteFile(g.out, outputBytes, 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan tersimpan di: %s\n", g.out)
	}

	if failOnIssues && report.HasCriticalIssues(pages, reportedIssues, site.PriorityPaths) {
		fmt.Fprintf(stderr, "\n[PERINGATAN] Ditemukan masalah kritis SEO pada situs %s (--fail-on-issues aktif: exit code 2).\n", site.Name)
		return 2
	}

	return 0
}

