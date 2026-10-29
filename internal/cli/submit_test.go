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

