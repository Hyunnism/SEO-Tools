package crawl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"seo-boost/internal/extract"
	"seo-boost/internal/model"
	"seo-boost/internal/sitemap"
)

const (
	DefaultConcurrency = 5
	DefaultDelayMS     = 200
	DefaultTimeoutS    = 20
	MaxRedirects       = 5
	MaxBodyBytes       = 5 * 1024 * 1024 // 5 MB
)

type Config struct {
	UserAgent   string
	Concurrency int
	DelayMS     int
	TimeoutS    int
	BaseURL     string
}

type Crawler struct {
	cfg        Config
	httpClient *http.Client
}

func NewCrawler(cfg Config, hc *http.Client) *Crawler {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.DelayMS < 0 {
		cfg.DelayMS = DefaultDelayMS
	}
	if cfg.TimeoutS <= 0 {
		cfg.TimeoutS = DefaultTimeoutS
	}

	if hc == nil {
		hc = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= MaxRedirects {
					return errors.New("terlalu banyak redirect (maksimal 5)")
				}
				return nil
			},
		}
	}

	return &Crawler{
		cfg:        cfg,
		httpClient: hc,
	}
}

type crawlResult struct {
	page          *model.Page
	internalLinks []string
}

// CrawlPages menjalankan worker pool untuk merayapi daftar URL yang diberikan.
func (c *Crawler) CrawlPages(ctx context.Context, urls []string, sitemapURLMap map[string]bool) ([]*model.Page, error) {
	total := len(urls)
	if total == 0 {
		return nil, nil
	}

	jobs := make(chan string, total)
	results := make(chan crawlResult, total)

	for _, u := range urls {
		jobs <- u
	}
	close(jobs)

	var wg sync.WaitGroup
	delay := time.Duration(c.cfg.DelayMS) * time.Millisecond

	for i := 0; i < c.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for targetURL := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				res := c.fetchAndExtract(ctx, targetURL, sitemapURLMap[targetURL])
				results <- res

				if delay > 0 {
					time.Sleep(delay)
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var pages []*model.Page
	// Peta untuk menghitung internal incoming links: targetURL -> jumlah link masuk
	incomingLinks := make(map[string]int)

	for res := range results {
		pages = append(pages, res.page)
		for _, link := range res.internalLinks {
			incomingLinks[link]++
		}
	}

	// Hitung InternalIn untuk setiap halaman
	for _, p := range pages {
		p.InternalIn = incomingLinks[p.URL]
		p.Audit = AuditPage(p)
	}

	// Evaluasi audit menyeluruh (misal duplicate title & meta)
	AuditSiteWide(pages)

	return pages, nil
}

func (c *Crawler) fetchAndExtract(ctx context.Context, targetURL string, inSitemap bool) crawlResult {
	reqTimeout := time.Duration(c.cfg.TimeoutS) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()

	u, err := url.Parse(targetURL)
	pagePath := "/"
	if err == nil && u.Path != "" {
		pagePath = u.Path
	}

	page := &model.Page{
		URL:       targetURL,
		Path:      pagePath,
		InSitemap: inSitemap,
	}

	// Coba fetch (dengan retry 1x untuk 5xx atau error jaringan)
	var resp *http.Response
	var finalURL string
	var fetchErr error

	for attempt := 1; attempt <= 2; attempt++ {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
		if err != nil {
			fetchErr = err
			break
		}
		if c.cfg.UserAgent != "" {
			req.Header.Set("User-Agent", c.cfg.UserAgent)
		}
		req.Header.Set("Accept", "text/html, application/xhtml+xml")

		resp, fetchErr = c.httpClient.Do(req)
		if fetchErr == nil {
			finalURL = resp.Request.URL.String()
			// Jika status bukan 5xx, tidak perlu retry
			if resp.StatusCode < 500 {
				break
			}
			resp.Body.Close()
		}
		// Jeda singkat sebelum retry
		time.Sleep(200 * time.Millisecond)
	}

	if fetchErr != nil {
		page.Crawl = &model.CrawlInfo{
			FetchedAt: time.Now(),
			Err:       fetchErr.Error(),
		}
		return crawlResult{page: page}
	}
	defer resp.Body.Close()

	crawlInfo := &model.CrawlInfo{
		FetchedAt:  time.Now(),
		HTTPStatus: resp.StatusCode,
		FinalURL:   finalURL,
	}

	contentType := resp.Header.Get("Content-Type")
	// Hanya parse body jika status OK dan berformat HTML
	if resp.StatusCode == http.StatusOK && (strings.Contains(contentType, "text/html") || strings.Contains(contentType, "xhtml")) {
		limitedBody := io.LimitReader(resp.Body, MaxBodyBytes)
		extracted, rawLinks, err := extract.ExtractHTML(limitedBody, targetURL)
		if err == nil {
			crawlInfo.Title = extracted.Title
			crawlInfo.MetaDesc = extracted.MetaDesc
			crawlInfo.Canonical = extracted.Canonical
			crawlInfo.RobotsMeta = extracted.RobotsMeta
			crawlInfo.H1 = extracted.H1
			crawlInfo.H2H3 = extracted.H2H3
			crawlInfo.WordCount = extracted.WordCount
			crawlInfo.ImagesNoAlt = extracted.ImagesNoAlt
			crawlInfo.HasJSONLD = extracted.HasJSONLD
			crawlInfo.ThinHTML = extracted.ThinHTML
			crawlInfo.Zones = extracted.Zones

			var internalLinks []string
			for _, rl := range rawLinks {
				norm, err := sitemap.NormalizeURL(rl, c.cfg.BaseURL)
				if err == nil {
					internalLinks = append(internalLinks, norm)
				}
			}
			page.Crawl = crawlInfo
			return crawlResult{page: page, internalLinks: internalLinks}
		}
		crawlInfo.Err = fmt.Sprintf("ekstraksi HTML gagal: %v", err)
	}

	page.Crawl = crawlInfo
	return crawlResult{page: page}
}
