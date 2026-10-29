package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"seo-boost/internal/google"
)

func TestRunOrchestrator_FullPipeline(t *testing.T) {
	// 1. Mock Server untuk Website
	var webServer *httptest.Server
	webServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/</loc></url>
  <url><loc>%s/blog/post-1</loc></url>
  <url><loc>%s/blog/orphan</loc></url>
</urlset>`, webServer.URL, webServer.URL, webServer.URL)
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Home Page Web</title><meta name="description" content="Ini meta description berukuran cukup"></head><body><h1>Selamat Datang</h1><a href="/blog/post-1">Post 1</a><a href="/broken-404">Broken Link</a><a href="/spa-app">App</a></body></html>`)
		case "/blog/post-1":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Tutorial Golang Terlengkap</title><meta name="description" content="Pelajari dasar bahasa pemrograman Go dan concurrency"></head><body><h1>Tutorial Golang</h1><p>Artikel ini membahas tutorial golang untuk pemula sampai mahir.</p></body></html>`)
		case "/blog/orphan":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Halaman Orphan</title><meta name="description" content="Halaman tanpa internal in"></head><body><h1>Halaman Orphan</h1><p>Tidak ada link masuk internal ke halaman ini.</p></body></html>`)
		case "/spa-app":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>SPA</title></head><body><div id="root"></div></body></html>`)
		case "/broken-404":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer webServer.Close()

	// 2. Mock Server untuk Google Search Console API
	pemKey := generateTestRSAKeyPEM(t)
	mockGSCServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token-12345",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/v1/urlInspection/index:inspect":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"inspectionResult": map[string]interface{}{
					"indexStatusResult": map[string]interface{}{
						"verdict":       "PASS",
						"coverageState": "Submitted and indexed",
						"crawledAs":     "MOBILE",
						"pageFetchState": "SUCCESSFUL",
					},
				},
			})
		case "/webmasters/v3/sites/sc-domain:example.com/searchAnalytics/query":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"rows": []map[string]interface{}{
					{
						"keys":        []string{webServer.URL + "/blog/post-1", "tutorial golang"},
						"clicks":      45,
						"impressions": 650,
						"ctr":         0.069,
						"position":    4.2,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockGSCServer.Close()

	// Setup custom HTTP transport untuk mengarahkan request Google ke mock server
	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockGSCServer.Listener.Addr().String()
		} else if req.URL.Host == "searchconsole.googleapis.com" || req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockGSCServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	// 3. Konfigurasi
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	sa := google.ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockGSCServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"defaults": {
			"crawl_concurrency": 2,
			"crawl_timeout_s": 5,
			"inspect_rpm": 600,
			"inspect_daily_budget": 50,
			"perf_days": 28,
			"stopwords": ["dan", "di", "ke", "dari", "ini"]
		},
		"sites": [
			{
				"name": "testsite",
				"base_url": "%s",
				"gsc_property": "sc-domain:example.com",
				"sitemaps": ["%s/sitemap.xml"]
			}
		]
	}`, filepath.ToSlash(dataDir), webServer.URL, webServer.URL)

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// 4. Jalankan End-to-End seo-boost run
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"run", "--config", cfgPath, "--site", "testsite", "--min-imp", "10"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("seo-boost run failed with code %d. Err: %s\nOut:\n%s", code, errBuf.String(), outBuf.String())
	}

	out := outBuf.String()

	// 5. Verifikasi semua tahapan tercetak
	expectedSteps := []string{
		"SEO-BOOST PIPELINE: testsite",
		"[1/6] Menemukan URL dari Sitemap & Spider...",
		"[2/6] Merayapi",
		"[3/6] Memeriksa Status Indeks ke Google Search Console...",
		"[4/6] Mengambil Metrik Performa Pencarian GSC",
		"[5/6] Menganalisis Peluang Keyword Gap",
		"[6/6] Menghitung Ringkasan, Diff, dan Menyimpan ke Store...",
		"SEO BOOST REPORT: testsite",
		"--- RINGKASAN SITUS ---",
	}
	for _, step := range expectedSteps {
		if !strings.Contains(out, step) {
			t.Errorf("expected output to contain %q, but not found", step)
		}
	}

	// 6. Strict check: pastikan tidak ada emoji/decorative unicode di output
	for i, r := range out {
		if r > unicode.MaxASCII {
			t.Errorf("forbidden non-ASCII rune %q (U+%04X) found at offset %d", r, r, i)
		}
	}

	// 7. Verifikasi persistensi store: pages.json dan runs/*.json harus ada
	pagesPath := filepath.Join(dataDir, "testsite", "pages.json")
	if _, err := os.Stat(pagesPath); os.IsNotExist(err) {
		t.Errorf("expected pages.json to exist at %s", pagesPath)
	}

	runsDir := filepath.Join(dataDir, "testsite", "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected at least 1 run file in %s, got err: %v", runsDir, err)
	}

	// 8. Jalankan run kedua untuk memverifikasi diff terhitung
	var outBuf2 bytes.Buffer
	code2 := Run([]string{"run", "--config", cfgPath, "--site", "testsite", "--min-imp", "10"}, &outBuf2, &errBuf, "0.1.0")
	if code2 != 0 {
		t.Fatalf("run 2 failed with code %d: %s", code2, errBuf.String())
	}
	out2 := outBuf2.String()
	if !strings.Contains(out2, "--- PERUBAHAN STATUS & PERINGKAT (DIFF) ---") {
		t.Errorf("expected diff section in run 2, got output: %s", out2)
	}
}

func TestRunOrchestrator_FailSoft(t *testing.T) {
	// Website server sederhana
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Home</title></head><body><h1>Home</h1></body></html>`)
	}))
	defer webServer.Close()

	// Mock Google yang sengaja error 500
	mockGSCServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
			})
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer mockGSCServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" || req.URL.Host == "searchconsole.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockGSCServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	pemKey := generateTestRSAKeyPEM(t)
	sa := google.ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockGSCServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "failsoft_site",
				"base_url": "%s",
				"gsc_property": "sc-domain:failsoft.com"
			}
		]
	}`, filepath.ToSlash(dataDir), webServer.URL)

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"run", "--config", cfgPath, "--site", "failsoft_site"}, &outBuf, &errBuf, "0.1.0")

	// Harus tetap exit 0 karena fail-soft: kegagalan API eksternal Google tidak mematikan pipeline
	if code != 0 {
		t.Fatalf("expected fail-soft exit code 0, got %d. Err: %s", code, errBuf.String())
	}

	if !strings.Contains(outBuf.String(), "SEO BOOST REPORT: failsoft_site") {
		t.Errorf("expected final report even when Google APIs fail")
	}
}

func TestRunOrchestrator_FreshStart(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Test Fresh</title></head><body><h1>Fresh</h1></body></html>`)
	}))
	defer webServer.Close()

	defaultHTTPClient = webServer.Client()
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "non_existent_data_dir") // Belum ada direktori ini sama sekali

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "fresh_site",
				"base_url": "%s",
				"gsc_property": "sc-domain:fresh.com"
			}
		]
	}`, filepath.ToSlash(dataDir), webServer.URL)

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"run", "--config", cfgPath, "--site", "fresh_site"}, &outBuf, &errBuf, "0.1.0")

	if code != 0 {
		t.Fatalf("expected fresh start exit code 0, got %d. Err: %s", code, errBuf.String())
	}

	// Direktori baru harus berhasil dibuat secara atomik
	if _, err := os.Stat(filepath.Join(dataDir, "fresh_site", "pages.json")); os.IsNotExist(err) {
		t.Errorf("fresh start should create pages.json cleanly")
	}
}

func TestRunOrchestrator_FormatMarkdownAndHTML(t *testing.T) {
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Format Test</title></head><body><h1>Testing Formats</h1></body></html>`)
	}))
	defer webServer.Close()

	defaultHTTPClient = webServer.Client()
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [{"name": "format_site", "base_url": "%s", "gsc_property": "sc-domain:format.com"}]
	}`, filepath.ToSlash(dataDir), webServer.URL)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	mdPath := filepath.Join(dir, "run_report.md")
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"run", "--config", cfgPath, "--site", "format_site", "--format", "md", "--out", mdPath}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("run md failed with code %d: %s", code, errBuf.String())
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("markdown report was not created at %s: %v", mdPath, err)
	}

	htmlPath := filepath.Join(dir, "run_report.html")
	outBuf.Reset()
	errBuf.Reset()
	code = Run([]string{"run", "--config", cfgPath, "--site", "format_site", "--format", "html", "--out", htmlPath}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("run html failed with code %d: %s", code, errBuf.String())
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Errorf("html report was not created at %s: %v", htmlPath, err)
	}
}

func TestRunOrchestrator_FailOnIssuesExitCode2(t *testing.T) {
	// Web server dengan halaman 404 (menghasilkan audit issue atau HTTP non-200)
	webServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Root</title></head><body><a href="/missing">Link</a></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer webServer.Close()

	defaultHTTPClient = webServer.Client()
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [{"name": "broken_site", "base_url": "%s", "gsc_property": "sc-domain:broken.com"}]
	}`, filepath.ToSlash(dataDir), webServer.URL)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// 1. Tanpa flag --fail-on-issues: harus exit 0
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"run", "--config", cfgPath, "--site", "broken_site"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("expected code 0 without flag, got %d: %s", code, errBuf.String())
	}

	// 2. Dengan flag --fail-on-issues: karena ada halaman 404, harus exit 2!
	outBuf.Reset()
	errBuf.Reset()
	code = Run([]string{"run", "--config", cfgPath, "--site", "broken_site", "--fail-on-issues"}, &outBuf, &errBuf, "0.1.0")
	if code != 2 {
		t.Fatalf("expected exit code 2 with --fail-on-issues, got %d (err: %s)", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "exit code 2") {
		t.Errorf("expected warning about exit code 2 in stderr: %s", errBuf.String())
	}
}

