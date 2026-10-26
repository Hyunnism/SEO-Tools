package crawl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"seo-boost/internal/model"
)

func TestCrawler_SuccessAndIncomingLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Halaman Beranda Panjang Karakter Pas</title><meta name="description" content="Ini meta description yang panjangnya cukup di atas batas tujuh puluh karakter rekomendasi."><link rel="canonical" href="`+r.URL.Scheme+`://`+r.Host+`/"><script type="application/ld+json">{}</script></head><body><h1>Judul Utama</h1><p>`+strings.Repeat("kata ", 350)+`</p><a href="/blog">Blog</a></body></html>`)
		case "/blog":
			fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Halaman Blog Informasi Menarik Sekali</title><meta name="description" content="Deskripsi blog yang cukup panjang dan informatif bagi para pembaca setia kami."><link rel="canonical" href="`+r.URL.Scheme+`://`+r.Host+`/blog"><script type="application/ld+json">{}</script></head><body><h1>Judul Blog</h1><p>`+strings.Repeat("artikel ", 350)+`</p><a href="/">Beranda</a></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	crawler := NewCrawler(Config{
		BaseURL:     server.URL,
		Concurrency: 2,
		DelayMS:     10,
		TimeoutS:    5,
	}, server.Client())

	urls := []string{server.URL + "/", server.URL + "/blog"}
	sitemapMap := map[string]bool{server.URL + "/": true, server.URL + "/blog": true}

	pages, err := crawler.CrawlPages(context.Background(), urls, sitemapMap)
	if err != nil {
		t.Fatalf("CrawlPages failed: %v", err)
	}

	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(pages))
	}

	for _, p := range pages {
		if p.Crawl.HTTPStatus != 200 {
			t.Errorf("URL %s status = %d, want 200", p.URL, p.Crawl.HTTPStatus)
		}
		if p.InternalIn != 1 {
			t.Errorf("URL %s InternalIn = %d, want 1", p.URL, p.InternalIn)
		}
		// Cek tidak ada error severity berat
		for _, issue := range p.Audit {
			if issue.Severity == "error" {
				t.Errorf("URL %s ada issue error: %s (%s)", p.URL, issue.Code, issue.Detail)
			}
		}
	}
}

func TestCrawler_RetryOn5xx(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att == 1 {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>OK Title Yang Cukup Panjang Karakter</title></head><body><h1>OK</h1></body></html>`)
	}))
	defer server.Close()

	crawler := NewCrawler(Config{BaseURL: server.URL, Concurrency: 1, DelayMS: 0, TimeoutS: 5}, server.Client())
	pages, err := crawler.CrawlPages(context.Background(), []string{server.URL + "/retry"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d, want 2 (retry 1x)", attempts)
	}
	if len(pages) != 1 || pages[0].Crawl.HTTPStatus != 200 {
		t.Errorf("expected 200 after retry, got: %+v", pages[0].Crawl)
	}
}

func TestCrawler_RedirectLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	crawler := NewCrawler(Config{BaseURL: server.URL, Concurrency: 1}, nil)
	pages, err := crawler.CrawlPages(context.Background(), []string{server.URL + "/loop"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(pages) != 1 || pages[0].Crawl.Err == "" {
		t.Errorf("harus mencatat error redirect loop, got: %+v", pages[0].Crawl)
	}
}

func TestCrawler_ContentTypeNonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, `%PDF-1.4 binary content`)
	}))
	defer server.Close()

	crawler := NewCrawler(Config{BaseURL: server.URL}, server.Client())
	pages, err := crawler.CrawlPages(context.Background(), []string{server.URL + "/doc.pdf"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if pages[0].Crawl.Title != "" {
		t.Errorf("PDF tidak boleh memiliki Title HTML, got: %s", pages[0].Crawl.Title)
	}
}

func TestSpiderBFS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprint(w, "User-agent: *\nDisallow: /admin\n")
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/page-a">A</a><a href="/admin">Admin</a><a href="https://external.com">Ext</a>`)
		case "/page-a":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/page-b">B</a>`)
		case "/page-b":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/">Home</a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	found, err := SpiderBFS(context.Background(), server.Client(), server.URL, "test-bot", 10)
	if err != nil {
		t.Fatalf("SpiderBFS failed: %v", err)
	}

	foundMap := make(map[string]bool)
	for _, u := range found {
		foundMap[u] = true
	}

	if !foundMap[server.URL] || !foundMap[server.URL+"/page-a"] || !foundMap[server.URL+"/page-b"] {
		t.Errorf("daftar halaman spider tidak lengkap: %v", found)
	}
	if foundMap[server.URL+"/admin"] {
		t.Error("/admin harus diabaikan karena disallow di robots.txt")
	}
}

func TestAuditPage_AllRules(t *testing.T) {
	// 1. HTTP not 200
	p1 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 404}}
	issues := AuditPage(p1)
	assertIssue(t, issues, "http_not_200", "error")

	// 2. Noindex
	p2 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, RobotsMeta: "noindex, nofollow"}}
	assertIssue(t, AuditPage(p2), "noindex_meta", "error")

	// 3. Title Missing & Short
	p3 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: ""}}
	assertIssue(t, AuditPage(p3), "title_missing", "error")

	p4 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Pendek"}}
	assertIssue(t, AuditPage(p4), "title_length", "warn")

	// 4. Meta Missing & Short
	p5 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", MetaDesc: ""}}
	assertIssue(t, AuditPage(p5), "meta_missing", "warn")

	// 5. H1 Missing & Multiple
	p6 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", H1: []string{}}}
	assertIssue(t, AuditPage(p6), "h1_missing", "error")

	p7 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", H1: []string{"H1 A", "H1 B"}}}
	assertIssue(t, AuditPage(p7), "h1_multiple", "warn")

	// 6. Canonical Missing & Mismatch
	p8 := &model.Page{URL: "https://example.com/a", Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", Canonical: ""}}
	assertIssue(t, AuditPage(p8), "canonical_missing", "warn")

	p9 := &model.Page{URL: "https://example.com/a", Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", Canonical: "https://example.com/b"}}
	assertIssue(t, AuditPage(p9), "canonical_mismatch", "warn")

	// 7. Images without alt
	p10 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", ImagesNoAlt: 3}}
	assertIssue(t, AuditPage(p10), "images_no_alt", "warn")

	// 8. Thin content & SPA
	p11 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", WordCount: 50}}
	assertIssue(t, AuditPage(p11), "thin_content", "warn")

	p12 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", ThinHTML: true}}
	assertIssue(t, AuditPage(p12), "thin_html_spa", "info")

	// 9. JSON-LD missing
	p13 := &model.Page{Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup", HasJSONLD: false}}
	assertIssue(t, AuditPage(p13), "jsonld_missing", "info")

	// 10. Orphan page
	p14 := &model.Page{InSitemap: true, InternalIn: 0, Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Standar Yang Panjangnya Cukup"}}
	assertIssue(t, AuditPage(p14), "orphan_page", "warn")
}

func TestAuditSiteWide_Duplicates(t *testing.T) {
	pages := []*model.Page{
		{URL: "https://example.com/1", Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Sama", MetaDesc: "Desc Sama"}},
		{URL: "https://example.com/2", Crawl: &model.CrawlInfo{HTTPStatus: 200, Title: "Judul Sama", MetaDesc: "Desc Sama"}},
	}
	AuditSiteWide(pages)

	assertIssue(t, pages[0].Audit, "title_duplicate", "warn")
	assertIssue(t, pages[0].Audit, "meta_duplicate", "warn")
	assertIssue(t, pages[1].Audit, "title_duplicate", "warn")
	assertIssue(t, pages[1].Audit, "meta_duplicate", "warn")
}

func assertIssue(t *testing.T, issues []model.AuditIssue, code, severity string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Severity == severity {
			return
		}
	}
	t.Errorf("isu dengan code %q dan severity %q tidak ditemukan dalam %+v", code, severity, issues)
}
