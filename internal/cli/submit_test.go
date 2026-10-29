package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"seo-boost/internal/google"
	"seo-boost/internal/model"
	"seo-boost/internal/store"
)

func TestIndexNowKeyCmd(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"indexnow-key"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("indexnow-key failed with code %d: %s", code, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "INDEXNOW KEY GENERATOR") {
		t.Errorf("expected header in output: %s", out)
	}
	if !strings.Contains(out, "Langkah integrasi") || !strings.Contains(out, "indexnow_key") {
		t.Errorf("expected instructions in output: %s", out)
	}
}

func TestSubmitCmd_GSCAndDryRun(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	var receivedGSCMethod string
	mockGSC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
			})
		default:
			receivedGSCMethod = r.Method
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// GET sitemaps list
			w.Header().Set("Content-Type", "application/json")
			now := time.Now().UTC()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sitemap": []map[string]interface{}{
					{
						"path":           "https://example.com/sitemap.xml",
						"lastDownloaded": now.Format(time.RFC3339),
						"isPending":      false,
						"warnings":       "0",
						"errors":         "0",
						"contents": []map[string]interface{}{
							{
								"type":      "web",
								"submitted": "10",
								"indexed":   "10",
							},
						},
					},
				},
			})
		}
	}))
	defer mockGSC.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" || req.URL.Host == "www.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockGSC.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockGSC.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "example",
				"base_url": "https://example.com",
				"gsc_property": "sc-domain:example.com",
				"sitemaps": ["https://example.com/sitemap.xml"]
			}
		]
	}`, filepath.ToSlash(dataDir))

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// 1. Test Dry Run
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "example", "--gsc", "--dry-run"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("submit dry-run failed with code %d: %s", code, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "[DRY RUN] [GSC]") {
		t.Errorf("expected dry-run marker in output: %s", outBuf.String())
	}

	// 2. Test Live Submit ke GSC
	outBuf.Reset()
	errBuf.Reset()
	code = Run([]string{"submit", "--config", cfgPath, "--site", "example", "--gsc"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("submit live failed with code %d: %s", code, errBuf.String())
	}
	out := outBuf.String()
	if !strings.Contains(out, "[OK] [GSC] Berhasil mengirim sinyal sitemap") {
		t.Errorf("expected success message in output: %s", out)
	}
	if !strings.Contains(out, "Status Sitemap di Google Search Console") {
		t.Errorf("expected sitemap status table in output: %s", out)
	}
	if receivedGSCMethod != http.MethodGet && receivedGSCMethod != http.MethodPut {
		t.Errorf("expected GSC requests, got: %s", receivedGSCMethod)
	}
}

func TestSubmitCmd_IndexNow(t *testing.T) {
	key := "1234567890abcdef1234567890abcdef"

	// Mock website host yang melayani key file
	var mockWeb *httptest.Server
	mockWeb = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+key+".txt" {
			fmt.Fprintln(w, key)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockWeb.Close()

	// Mock IndexNow endpoint
	var receivedURLs []string
	mockIndexNow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Host    string   `json:"host"`
			Key     string   `json:"key"`
			URLList []string `json:"urlList"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		receivedURLs = payload.URLList
		w.WriteHeader(http.StatusOK)
	}))
	defer mockIndexNow.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.indexnow.org" {
			req.URL.Scheme = "http"
			req.URL.Host = mockIndexNow.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "mockweb",
				"base_url": "%s",
				"gsc_property": "sc-domain:mockweb.com",
				"indexnow_key": "%s"
			}
		]
	}`, filepath.ToSlash(dataDir), mockWeb.URL, key)

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	// Simpan pages di store
	st := store.NewStore(dataDir)
	_ = st.SavePages("mockweb", []*model.Page{
		{
			URL: mockWeb.URL + "/halaman-baru",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
	})

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "mockweb", "--indexnow"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("submit indexnow failed with code %d: %s", code, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "[OK] File kunci IndexNow terverifikasi valid") {
		t.Errorf("expected key verification success in output: %s", out)
	}
	if !strings.Contains(out, "[OK] [INDEXNOW] URL berhasil diterima oleh IndexNow") {
		t.Errorf("expected submit success in output: %s", out)
	}
	if len(receivedURLs) != 1 || receivedURLs[0] != mockWeb.URL+"/halaman-baru" {
		t.Errorf("unexpected received URLs: %v", receivedURLs)
	}
}

func TestSubmitCmd_IndexNowKeyMismatch(t *testing.T) {
	key := "1234567890abcdef1234567890abcdef"

	// Mock website host dengan kunci yang salah di server
	var mockWeb *httptest.Server
	mockWeb = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "kunci-salah-123")
	}))
	defer mockWeb.Close()

	defaultHTTPClient = mockWeb.Client()
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "mockweb",
				"base_url": "%s",
				"gsc_property": "sc-domain:mockweb.com",
				"indexnow_key": "%s"
			}
		]
	}`, filepath.ToSlash(dataDir), mockWeb.URL, key)

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "mockweb", "--indexnow"}, &outBuf, &errBuf, "0.1.0")

	// Harus gagal exit code 1 dan membatalkan submit
	if code != 1 {
		t.Fatalf("expected code 1 on key mismatch, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "Verifikasi file kunci IndexNow gagal") || !strings.Contains(errBuf.String(), "dibatalkan") {
		t.Errorf("expected cancellation message, got: %s", errBuf.String())
	}
}

func TestSubmitCmd_GoogleIndexing_TwoKeyValidation(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)

	// Config dengan google_indexing enabled=false (default)
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"sites": [
			{
				"name": "example",
				"base_url": "https://example.com",
				"gsc_property": "sc-domain:example.com"
			}
		]
	}`, filepath.ToSlash(dataDir))

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "example", "--google-indexing"}, &outBuf, &errBuf, "0.1.0")

	if code != 1 {
		t.Fatalf("expected exit code 1 when two-key is not satisfied, got %d", code)
	}
	if !strings.Contains(errBuf.String(), "kunci 1 non-aktif") {
		t.Errorf("expected two-key error message in stderr, got: %s", errBuf.String())
	}
}

func TestSubmitCmd_GoogleIndexing_DryRunAndPolicyWarning(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600)

	// Config dengan google_indexing enabled=true
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"google_indexing": {
			"enabled": true,
			"daily_cap": 25
		},
		"sites": [
			{
				"name": "example",
				"base_url": "https://example.com",
				"gsc_property": "sc-domain:example.com"
			}
		]
	}`, filepath.ToSlash(dataDir))

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	st := store.NewStore(dataDir)
	_ = st.SavePages("example", []*model.Page{
		{
			URL: "https://example.com/page-1",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
	})

	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "example", "--google-indexing", "--dry-run"}, &outBuf, &errBuf, "0.1.0")

	if code != 0 {
		t.Fatalf("expected exit code 0 on dry run, got %d: %s", code, errBuf.String())
	}

	out := outBuf.String()
	if !strings.Contains(out, "[POLICY] [GOOGLE_INDEXING_API]") {
		t.Errorf("expected policy warning header in output: %s", out)
	}
	if strings.Count(out, "[POLICY] [GOOGLE_INDEXING_API]") != 1 {
		t.Errorf("policy warning should appear exactly once, got count %d", strings.Count(out, "[POLICY] [GOOGLE_INDEXING_API]"))
	}
	if !strings.Contains(out, "[DRY RUN] [INDEXING]") {
		t.Errorf("expected dry run marker in output: %s", out)
	}
	if !strings.Contains(out, "https://example.com/page-1") {
		t.Errorf("expected candidate url in dry run output: %s", out)
	}
}

func TestSubmitCmd_GoogleIndexing_LivePublishAndFailFast(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	var requestCount int
	var shouldFail403 bool
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
			})
		case "/v3/urlNotifications:publish":
			requestCount++
			if shouldFail403 {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error": {"code": 403, "message": "Permission denied"}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"urlNotificationMetadata": {"url": "https://example.com/p1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" || req.URL.Host == "indexing.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"google_indexing": {
			"enabled": true,
			"daily_cap": 10
		},
		"sites": [
			{
				"name": "example",
				"base_url": "https://example.com",
				"gsc_property": "sc-domain:example.com"
			}
		]
	}`, filepath.ToSlash(dataDir))

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	st := store.NewStore(dataDir)
	_ = st.SavePages("example", []*model.Page{
		{
			URL: "https://example.com/p1",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		{
			URL: "https://example.com/p2",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
	})

	// 1. Live publish sukses
	var outBuf, errBuf bytes.Buffer
	code := Run([]string{"submit", "--config", cfgPath, "--site", "example", "--google-indexing"}, &outBuf, &errBuf, "0.1.0")
	if code != 0 {
		t.Fatalf("expected code 0 on live publish, got %d: %s", code, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "[+] [URL_UPDATED] https://example.com/p1") {
		t.Errorf("expected URL_UPDATED publish log, got: %s", outBuf.String())
	}

	// 2. Fail-Fast on HTTP 403
	shouldFail403 = true
	requestCount = 0
	outBuf.Reset()
	errBuf.Reset()

	// Tambahkan URL baru p3 dan p4 yang belum pernah dikirim
	_ = st.SavePages("example", []*model.Page{
		{
			URL: "https://example.com/p3",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		{
			URL: "https://example.com/p4",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
	})

	_ = Run([]string{"submit", "--config", cfgPath, "--site", "example", "--google-indexing", "--aggressive"}, &outBuf, &errBuf, "0.1.0")
	if !strings.Contains(errBuf.String(), "[403 FORBIDDEN]") {
		t.Errorf("expected 403 forbidden error message, got: %s", errBuf.String())
	}
	// Fail-fast harus langsung berhenti setelah URL pertama gagal 403, tidak melanjutkan URL kedua
	if requestCount != 1 {
		t.Errorf("expected fail-fast to stop after 1 request, got %d requests", requestCount)
	}
}

func TestSubmitCmd_GoogleIndexing_FailFast429_And_Filters(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	var requestCount int
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
			})
		case "/v3/urlNotifications:publish":
			requestCount++
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": {"code": 429, "message": "Rate limit exceeded"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" || req.URL.Host == "indexing.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = mockServer.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	defaultHTTPClient = &http.Client{Transport: customTransport}
	defer func() { defaultHTTPClient = nil }()

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    mockServer.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)
	_ = os.WriteFile(filepath.Join(dir, "sa.json"), saBytes, 0o600)

	cfgContent := fmt.Sprintf(`{
		"service_account_file": "./sa.json",
		"data_dir": "%s",
		"google_indexing": {
			"enabled": true,
			"daily_cap": 10
		},
		"sites": [
			{
				"name": "example",
				"base_url": "https://example.com",
				"gsc_property": "sc-domain:example.com"
			}
		]
	}`, filepath.ToSlash(dataDir))

	cfgPath := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o600)

	st := store.NewStore(dataDir)
	_ = st.SavePages("example", []*model.Page{
		// URL valid 1
		{
			URL: "https://example.com/valid-1",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		// URL valid 2
		{
			URL: "https://example.com/valid-2",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		// URL non-200 (harus difilter, tidak pernah dikirim)
		{
			URL: "https://example.com/error-500",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 500,
			},
		},
		// URL noindex (harus difilter, tidak pernah dikirim)
		{
			URL: "https://example.com/no-index",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				RobotsMeta: "noindex, follow",
			},
		},
	})

	var outBuf, errBuf bytes.Buffer
	_ = Run([]string{"submit", "--config", cfgPath, "--site", "example", "--google-indexing"}, &outBuf, &errBuf, "0.1.0")

	if !strings.Contains(errBuf.String(), "[429 RATE LIMIT]") {
		t.Errorf("expected 429 rate limit error message, got: %s", errBuf.String())
	}
	// Google client melakukan maxRetries (4 retries) untuk URL pertama saat 429,
	// lalu fail-fast menghentikan loop sehingga URL kedua tidak pernah dikirim (requestCount tepat 5).
	if requestCount != 5 {
		t.Errorf("expected fail-fast on 429 after retries exhausted for 1st URL, got %d (should be 5)", requestCount)
	}
}



