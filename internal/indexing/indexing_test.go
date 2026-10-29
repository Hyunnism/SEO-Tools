package indexing

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/google"
	"seo-boost/internal/model"
)

func generateTestRSAKeyPEM(t *testing.T) string {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gagal membuat rsa key: %v", err)
	}
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(pk)
	if err != nil {
		t.Fatalf("gagal marshal pkcs8: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})
	return string(pemBlock)
}

func TestBuildQueue_PrioritiesAndSafetyGuards(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

	cfg := config.GoogleIndexingConfig{
		Enabled:                   true,
		Mode:                      "aggressive",
		DailyCap:                  50,
		ResubmitCooldownDays:      7,
		MaxSendsPerContentVersion: 3,
		SendDeleted:               true,
	}

	store := &SiteIndexingStore{
		Records: map[string]IndexingRecord{
			// Sudah dikirim 2 hari lalu (masih cooldown 7 hari)
			"https://example.com/cooldown": {
				URL:         "https://example.com/cooldown",
				SentAt:      now.Add(-2 * 24 * time.Hour),
				SendCount:   1,
				ContentHash: "hash-cooldown",
			},
			// Sudah dikirim 3x dengan hash yang sama (maks send reached)
			"https://example.com/max-sends": {
				URL:         "https://example.com/max-sends",
				SentAt:      now.Add(-10 * 24 * time.Hour), // lewat cooldown
				SendCount:   3,
				ContentHash: "hash-same",
			},
			// Sudah dikirim sebelumnya tetapi konten sekarang berubah
			"https://example.com/content-changed": {
				URL:         "https://example.com/content-changed",
				SentAt:      now.Add(-10 * 24 * time.Hour),
				SendCount:   1,
				ContentHash: "old-hash-123",
			},
		},
	}

	pages := []*model.Page{
		// 1. URL Baru (P1)
		{
			URL:  "https://example.com/new-page",
			Path: "/new-page",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				Title:      "Halaman Baru",
			},
		},
		// 2. Konten Berubah (P2)
		{
			URL:  "https://example.com/content-changed",
			Path: "/content-changed",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				Title:      "Judul Baru Berubah",
			},
		},
		// 3. Discovered Not Indexed (P3)
		{
			URL:  "https://example.com/discovered",
			Path: "/discovered",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				Title:      "Discovered",
			},
			Index: &model.IndexInfo{
				Status: model.StatusDiscoveredNotIndexed,
			},
		},
		// 4. Halaman dalam Cooldown (harus disaring / dibuang)
		{
			URL:  "https://example.com/cooldown",
			Path: "/cooldown",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		// 5. Halaman mencapai Max Sends (harus disaring / dibuang jika hash sama)
		{
			URL:  "https://example.com/max-sends",
			Path: "/max-sends",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		},
		// 6. Halaman Noindex (harus disaring / dibuang)
		{
			URL:  "https://example.com/noindex-page",
			Path: "/noindex-page",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				RobotsMeta: "noindex, follow",
			},
		},
		// 7. Halaman 404 yang sebelumnya tercatat (P6: URL_DELETED)
		{
			URL:  "https://example.com/deleted-article",
			Path: "/deleted-article",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 404,
			},
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
		},
	}

	// 1. Uji Mode Agresif
	queueAggressive := BuildQueue(pages, store, cfg, []string{"/priority"}, true, now)

	urlMap := make(map[string]PrioritizedIndexingItem)
	for _, item := range queueAggressive {
		urlMap[item.URL] = item
	}

	// Verifikasi pengaman: cooldown dan max-sends dan noindex TIDAK ADA dalam antrean
	if _, exists := urlMap["https://example.com/cooldown"]; exists {
		t.Errorf("cooldown URL should not be in queue")
	}
	if _, exists := urlMap["https://example.com/noindex-page"]; exists {
		t.Errorf("noindex URL should not be in queue")
	}

	// Verifikasi prioritas
	if item, exists := urlMap["https://example.com/new-page"]; !exists || item.Priority != 1 {
		t.Errorf("expected new-page to be Priority 1, got %+v", item)
	}
	if item, exists := urlMap["https://example.com/content-changed"]; !exists || item.Priority != 2 {
		t.Errorf("expected content-changed to be Priority 2, got %+v", item)
	}
	if item, exists := urlMap["https://example.com/discovered"]; !exists || item.Priority != 3 {
		t.Errorf("expected discovered to be Priority 3, got %+v", item)
	}
	if item, exists := urlMap["https://example.com/deleted-article"]; !exists || item.Priority != 6 || item.Type != TypeURLDeleted {
		t.Errorf("expected deleted-article to be Priority 6 (URL_DELETED), got %+v", item)
	}

	// 2. Uji Safe Mode (aggressive = false)
	queueSafe := BuildQueue(pages, store, cfg, []string{"/priority"}, false, now)
	for _, item := range queueSafe {
		if item.Priority > 2 {
			t.Errorf("safe mode should only include Priority 1 or 2, got priority %d (%s)", item.Priority, item.URL)
		}
	}
}

func TestBuildQueue_HardClampDailyCap(t *testing.T) {
	cfg := config.GoogleIndexingConfig{
		Enabled:  true,
		DailyCap: 300, // Di atas batas resmi Google 200
	}

	var pages []*model.Page
	for i := 0; i < 250; i++ {
		pages = append(pages, &model.Page{
			URL:  "https://example.com/page/" + string(rune('a'+i%26)) + "/" + string(rune('0'+i%10)) + "/" + string(rune(i)),
			Path: "/page",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
		})
	}

	queue := BuildQueue(pages, nil, cfg, nil, true, time.Now())
	if len(queue) > 200 {
		t.Errorf("expected daily_cap to be hard-clamped to 200, got: %d", len(queue))
	}
}

func TestPublishURLAndGetMetadata(t *testing.T) {
	pemKey := generateTestRSAKeyPEM(t)

	var lastReq PublishRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-token",
				"expires_in":   3600,
			})
		case "/v3/urlNotifications:publish":
			if r.Method != http.MethodPost {
				http.Error(w, "bad method", http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&lastReq)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(PublishResponse{
				UrlNotificationMetadata: UrlNotificationMetadata{
					URL: lastReq.URL,
					LatestUpdate: &NotificationRecord{
						URL:        lastReq.URL,
						Type:       lastReq.Type,
						NotifyTime: "2026-10-29T12:00:00Z",
					},
				},
			})
		case "/v3/urlNotifications/metadata":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(UrlNotificationMetadata{
				URL: r.URL.Query().Get("url"),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	customTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "oauth2.googleapis.com" || req.URL.Host == "indexing.googleapis.com" {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
		}
		return http.DefaultTransport.RoundTrip(req)
	})

	sa := google.ServiceAccountKey{
		ClientEmail: "tester@example.iam.gserviceaccount.com",
		PrivateKey:  pemKey,
		TokenURI:    server.URL + "/token",
	}
	saBytes, _ := json.Marshal(sa)

	client, err := google.NewClientFromBytes(saBytes, google.WithHTTPClient(&http.Client{Transport: customTransport}))
	if err != nil {
		t.Fatalf("gagal inisialisasi google client: %v", err)
	}

	ctx := context.Background()

	// 1. Publish URL
	resp, err := PublishURL(ctx, client, "https://example.com/artikel-1", TypeURLUpdated)
	if err != nil {
		t.Fatalf("PublishURL error: %v", err)
	}
	if resp.UrlNotificationMetadata.URL != "https://example.com/artikel-1" {
		t.Errorf("unexpected URL in response: %s", resp.UrlNotificationMetadata.URL)
	}
	if lastReq.Type != TypeURLUpdated {
		t.Errorf("expected TypeURLUpdated, got %s", lastReq.Type)
	}

	// 2. Get Metadata
	meta, err := GetMetadata(ctx, client, "https://example.com/artikel-1")
	if err != nil {
		t.Fatalf("GetMetadata error: %v", err)
	}
	if meta.URL != "https://example.com/artikel-1" {
		t.Errorf("unexpected URL in metadata: %s", meta.URL)
	}
}

func TestStoreSaveLoadAndLatency(t *testing.T) {
	dir := t.TempDir()

	store, err := LoadIndexingStore(dir, "mysite")
	if err != nil {
		t.Fatalf("LoadIndexingStore error: %v", err)
	}
	if store.Site != "mysite" {
		t.Errorf("expected site mysite, got %s", store.Site)
	}

	now := time.Now().UTC()
	store.Records["https://example.com/page1"] = IndexingRecord{
		URL:         "https://example.com/page1",
		Type:        TypeURLUpdated,
		ContentHash: "hash123",
		SentAt:      now.Add(-2 * time.Hour),
		HTTPStatus:  200,
		SendCount:   1,
	}

	if err := SaveIndexingStore(dir, "mysite", store); err != nil {
		t.Fatalf("SaveIndexingStore error: %v", err)
	}

	// Muat ulang
	reloaded, err := LoadIndexingStore(dir, "mysite")
	if err != nil {
		t.Fatalf("reload error: %v", err)
	}
	if len(reloaded.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(reloaded.Records))
	}

	// Hitung latensi crawl
	crawlTime := now.Add(-1 * time.Hour) // 1 jam setelah pengiriman
	pages := []*model.Page{
		{
			URL: "https://example.com/page1",
			Index: &model.IndexInfo{
				LastCrawlTime: &crawlTime,
			},
		},
	}

	latency, count := CalculateAverageCrawlLatency(pages, reloaded)
	if count != 1 {
		t.Fatalf("expected 1 sample for latency, got %d", count)
	}
	if latency != 1*time.Hour {
		t.Errorf("expected 1h latency, got %v", latency)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
