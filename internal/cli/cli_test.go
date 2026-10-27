package cli

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"seo-boost/internal/config"
	"seo-boost/internal/google"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, "dev")
	return code, out.String(), errb.String()
}

func generateTestRSAKeyPEM(t *testing.T) string {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
}

func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestNoArgs(t *testing.T) {
	code, _, errs := run()
	if code != 1 || !strings.Contains(errs, "Pemakaian") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errs := run("ngawur")
	if code != 1 || !strings.Contains(errs, "ngawur") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestNotYetAvailable(t *testing.T) {
	code, _, errs := run("gap")
	if code != 1 || !strings.Contains(errs, "belum tersedia") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestHelp(t *testing.T) {
	for _, a := range []string{"help", "--help", "-h"} {
		code, out, _ := run(a)
		if code != 0 || !strings.Contains(out, "Pemakaian") {
			t.Fatalf("%s: code=%d out=%q", a, code, out)
		}
	}
}

func TestDoctorConfigOnly(t *testing.T) {
	dir := t.TempDir()
	sa := `{"client_email":"a","private_key":"k","token_uri":"u"}`
	good := `{"service_account_file":"./sa.json","sites":[{"name":"a","gsc_property":"sc-domain:e.com","base_url":"https://e.com"}]}`
	bad := `{"service_account_file":"./sa.json","sites":[]}`
	for name, body := range map[string]string{"sa.json": sa, "good.json": good, "bad.json": bad} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if code, out, _ := run("doctor", "--config-only", "--config", filepath.Join(dir, "good.json")); code != 0 || !strings.Contains(out, "Config terstruktur dengan benar") {
		t.Fatalf("good: code=%d out=%q", code, out)
	}
	if code, _, errs := run("doctor", "--config-only", "--config", filepath.Join(dir, "bad.json")); code != 1 || !strings.Contains(errs, "minimal 1 site") {
		t.Fatalf("bad: code=%d err=%q", code, errs)
	}
	if code, _, errs := run("doctor", "--config-only", "--config", filepath.Join(dir, "tidak-ada.json")); code != 1 || errs == "" {
		t.Fatalf("missing: code=%d err=%q", code, errs)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDoctorOnlineMock(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"mock-token","expires_in":3600}`)
		case "/webmasters/v3/sites":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"siteEntry": [
					{"siteUrl": "sc-domain:example.com", "permissionLevel": "siteOwner"}
				]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	sa := google.ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	// Kasus 1: Site cocok dengan properti di GSC
	cfgGood := `{"service_account_file":"./sa.json","sites":[{"name":"portfolio","gsc_property":"sc-domain:example.com","base_url":"https://example.com"}]}`
	cfgGoodPath := filepath.Join(dir, "good.json")
	_ = os.WriteFile(cfgGoodPath, []byte(cfgGood), 0o600)

	code, out, _ := run("doctor", "--config", cfgGoodPath)
	if code != 0 {
		t.Fatalf("expected doctor sukses, got %d, out: %s", code, out)
	}
	if !strings.Contains(out, "Owner (Akses Penuh)") {
		t.Errorf("output tidak memuat Owner: %s", out)
	}

	// Kasus 2: Site tidak ada di GSC
	cfgMissing := `{"service_account_file":"./sa.json","sites":[{"name":"blog","gsc_property":"sc-domain:tidak-ada.com","base_url":"https://tidak-ada.com"}]}`
	cfgMissingPath := filepath.Join(dir, "missing.json")
	_ = os.WriteFile(cfgMissingPath, []byte(cfgMissing), 0o600)

	code, _, errs := run("doctor", "--config", cfgMissingPath)
	if code != 1 {
		t.Fatalf("expected doctor gagal karena site tidak terdaftar di GSC, got code %d", code)
	}
	if !strings.Contains(errs, "tidak ditemukan di daftar properti GSC") {
		t.Errorf("error output salah: %s", errs)
	}
}

func TestCheckGoVersion(t *testing.T) {
	if err := checkGoVersion("go1.21.0"); err == nil {
		t.Error("go1.21.0 harus gagal")
	}
	if err := checkGoVersion("go1.22.0"); err != nil {
		t.Errorf("go1.22.0 harus lolos: %v", err)
	}
	if err := checkGoVersion("go1.25.7"); err != nil {
		t.Errorf("go1.25.7 harus lolos: %v", err)
	}
}

func TestVerboseLogger(t *testing.T) {
	var b bytes.Buffer
	if newLogger(&b, false).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus mati tanpa --verbose")
	}
	if !newLogger(&b, true).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus nyala dengan --verbose")
	}
}

func TestDiscoverAndCrawlCmd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			fmt.Fprintln(w, "User-agent: *")
			fmt.Fprintf(w, "Sitemap: http://%s/sitemap.xml\n", r.Host)
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
	<url><loc>http://%s/</loc></url>
	<url><loc>http://%s/about</loc></url>
</urlset>`, r.Host, r.Host)
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Home Page</title><meta name="description" content="Welcome to homepage"></head><body><h1>Main Title</h1><p>Welcome to our site</p><a href="/about">About</a><a href="/orphan">Orphan</a></body></html>`)
		case "/about":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>About Us</title><meta name="description" content="About us page"></head><body><h1>About Us</h1><p>Our company details.</p></body></html>`)
		case "/orphan":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Orphan Page</title></head><body><h1>Orphan</h1><p>Not in sitemap</p></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	defaultHTTPClient = ts.Client()
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"sites": [
			{
				"name": "mocksite",
				"base_url": "%s",
				"gsc_property": "%s/"
			}
		]
	}`, ts.URL, ts.URL)
	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// Test discover tanpa spider
	code, out, errs := run("discover", "--config", cfgPath)
	if code != 0 {
		t.Fatalf("discover failed code=%d out=%s err=%s", code, out, errs)
	}
	if !strings.Contains(out, "Sitemap URLs: 2") {
		t.Errorf("expected 2 sitemap URLs, got: %s", out)
	}

	// Test discover dengan --spider
	code, out, _ = run("discover", "--config", cfgPath, "--spider")
	if code != 0 {
		t.Fatalf("discover with spider failed code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "Spider URLs:") || !strings.Contains(out, "TIDAK di sitemap") {
		t.Errorf("expected spider results, got: %s", out)
	}

	// Test crawl
	code, out, errs = run("crawl", "--config", cfgPath, "--concurrency", "2")
	if code != 0 {
		t.Fatalf("crawl failed code=%d err=%s out=%s", code, errs, out)
	}
	if !strings.Contains(out, "=== Hasil Crawl & On-Page Audit") {
		t.Errorf("crawl output invalid: %s", out)
	}
	if !strings.Contains(out, "Total Halaman Dirayapi:") {
		t.Errorf("crawl missing total pages: %s", out)
	}
}

func TestResolveSite(t *testing.T) {
	// 1. Config kosong
	cfgEmpty := &config.Config{Sites: nil}
	if _, err := resolveSite(cfgEmpty, ""); err == nil {
		t.Error("expected error on empty sites")
	}

	// 2. Satu site, tanpa argumen
	cfgSingle := &config.Config{Sites: []config.Site{{Name: "mysite"}}}
	s, err := resolveSite(cfgSingle, "")
	if err != nil || s.Name != "mysite" {
		t.Errorf("failed resolving single site: %v, %v", s, err)
	}

	// 3. Banyak site, tanpa argumen -> harus error
	cfgMulti := &config.Config{Sites: []config.Site{{Name: "s1"}, {Name: "s2"}}}
	if _, err := resolveSite(cfgMulti, ""); err == nil {
		t.Error("expected error when multiple sites exist without --site flag")
	}

	// 4. Banyak site, dengan argumen cocok
	s, err = resolveSite(cfgMulti, "s2")
	if err != nil || s.Name != "s2" {
		t.Errorf("failed resolving s2: %v, %v", s, err)
	}

	// 5. Banyak site, nama tidak ditemukan
	if _, err := resolveSite(cfgMulti, "s3"); err == nil {
		t.Error("expected error for nonexistent site")
	}
}

func TestInspectAndPerfCmd(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"mock-token","expires_in":3600}`)
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "User-agent: *\nSitemap: http://%s/sitemap.xml\n", r.Host)
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
	<url><loc>http://%s/</loc></url>
</urlset>`, r.Host)
		case "/v1/urlInspection/index:inspect":
			fmt.Fprint(w, `{
				"inspectionResult": {
					"inspectionUrl": "http://127.0.0.1/",
					"indexStatusResult": {
						"verdict": "PASS",
						"coverageState": "Submitted and indexed",
						"pageFetchState": "SUCCESSFUL"
					}
				}
			}`)
		default:
			if strings.Contains(r.URL.Path, "searchAnalytics/query") {
				fmt.Fprint(w, `{"rows":[
					{"keys":["http://127.0.0.1/","seo keyword"],"clicks":12,"impressions":100,"ctr":0.12,"position":4.5}
				]}`)
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "searchconsole.googleapis.com" || req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	sa := google.ServiceAccountKey{
		ClientEmail: "test-bot@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"defaults": {
			"inspect_rpm": 600,
			"inspect_daily_budget": 50,
			"perf_days": 28
		},
		"sites": [
			{
				"name": "example",
				"base_url": "http://%s",
				"gsc_property": "sc-domain:example.com"
			}
		]
	}`, filepath.ToSlash(filepath.Join(dir, "data")), mockServer.Listener.Addr().String())

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// 1. Test inspect --dry-run
	code, out, errs := run("inspect", "--config", cfgPath, "--dry-run")
	if code != 0 {
		t.Fatalf("inspect dry-run failed code=%d err=%s out=%s", code, errs, out)
	}
	if !strings.Contains(out, "[DRY RUN]") || !strings.Contains(out, "sisa budget (Budget Harian: 50)") {
		t.Errorf("inspect dry-run output invalid: %s", out)
	}

	// 2. Test inspect live run
	code, out, errs = run("inspect", "--config", cfgPath, "--limit", "1")
	if code != 0 {
		t.Fatalf("inspect run failed code=%d err=%s out=%s", code, errs, out)
	}
	if !strings.Contains(out, "Selesai menginspeksi") || !strings.Contains(out, "indexed: 1") {
		t.Errorf("inspect run output invalid: %s", out)
	}

	// 3. Test perf run
	code, out, errs = run("perf", "--config", cfgPath, "--days", "28")
	if code != 0 {
		t.Fatalf("perf run failed code=%d err=%s out=%s", code, errs, out)
	}
	if !strings.Contains(out, "=== Performa Pencarian GSC") || !strings.Contains(out, "Total Klik: 12") {
		t.Errorf("perf run output invalid: %s", out)
	}
}
