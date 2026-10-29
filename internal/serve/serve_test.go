package serve_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/model"
	"seo-boost/internal/serve"
	"seo-boost/internal/store"
)

func setupTestServer(t *testing.T, token string, requireAuth bool, runner serve.Runner) (*serve.Server, string) {
	t.Helper()
	tmpDir := t.TempDir()

	cfg := &config.Config{
		DataDir: tmpDir,
		Sites: []config.Site{
			{
				Name:        "test-site",
				BaseURL:     "https://example.com",
				GSCProperty: "sc-domain:example.com",
			},
		},
	}

	st := store.NewStore(tmpDir)

	// Seed dummy page data
	pages := []*model.Page{
		{
			URL:        "https://example.com/",
			Path:       "/",
			InSitemap:  true,
			InternalIn: 5,
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				Title:      "Homepage <script>alert('xss')</script>",
			},
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
			Perf: &model.PagePerf{
				Clicks:      10,
				Impressions: 100,
				Position:    3.2,
			},
			Gaps: []model.QueryGap{
				{
					QueryPerf: model.QueryPerf{
						Query:       "test keyword",
						Impressions: 120,
						Position:    4.1,
					},
					Score: 85.5,
				},
			},
		},
		{
			URL:        "https://example.com/blog",
			Path:       "/blog",
			InSitemap:  true,
			InternalIn: 2,
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
				Title:      "Blog Page",
			},
			Index: &model.IndexInfo{
				Status: model.StatusCrawledNotIndexed,
			},
		},
	}
	if err := st.SavePages("test-site", pages); err != nil {
		t.Fatalf("gagal simpan test pages: %v", err)
	}

	runSummary := store.BuildRunSummary("test-site", pages)
	if err := st.SaveRun("test-site", runSummary); err != nil {
		t.Fatalf("gagal simpan test run: %v", err)
	}

	srv := serve.NewServer(cfg, st, runner, token, requireAuth)
	return srv, tmpDir
}

func TestIsLoopbackAndValidateAddr(t *testing.T) {
	loopbacks := []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1"}
	for _, addr := range loopbacks {
		if !serve.IsLoopback(addr) {
			t.Errorf("diharapkan %s adalah loopback", addr)
		}
	}

	nonLoopbacks := []string{"0.0.0.0:8080", "192.168.1.100:8080", "example.com:8080"}
	for _, addr := range nonLoopbacks {
		if serve.IsLoopback(addr) {
			t.Errorf("diharapkan %s bukan loopback", addr)
		}
	}

	// ValidateAddr: non-loopback tanpa token harus ditolak
	if err := serve.ValidateAddr("0.0.0.0:8080", "", true); err == nil {
		t.Errorf("diharapkan error untuk non-loopback tanpa token")
	}
	if err := serve.ValidateAddr("0.0.0.0:8080", "valid-token", true); err != nil {
		t.Errorf("diharapkan sukses untuk non-loopback dengan token, dapat: %v", err)
	}
	if err := serve.ValidateAddr("127.0.0.1:8080", "", false); err != nil {
		t.Errorf("diharapkan sukses untuk loopback tanpa token, dapat: %v", err)
	}
}

func TestAuthMiddleware(t *testing.T) {
	const secretToken = "super-secret-token-12345"
	srv, _ := setupTestServer(t, secretToken, true, nil)
	handler := srv.Handler()

	// 1. Tanpa token -> 401
	req1 := httptest.NewRequest("GET", "/api/sites", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("diharapkan 401 Unauthorized, dapat: %d", rec1.Code)
	}

	// 2. Token salah -> 401
	req2 := httptest.NewRequest("GET", "/api/sites?token=wrong-token", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("diharapkan 401 Unauthorized untuk token salah, dapat: %d", rec2.Code)
	}

	// 3. Query param token -> 200 + set cookie
	req3 := httptest.NewRequest("GET", "/api/sites?token="+secretToken, nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("diharapkan 200 OK dengan query token, dapat: %d", rec3.Code)
	}
	cookies := rec3.Result().Cookies()
	var foundCookie bool
	for _, c := range cookies {
		if c.Name == "seo_boost_token" && c.Value == secretToken {
			foundCookie = true
			break
		}
	}
	if !foundCookie {
		t.Errorf("diharapkan cookie seo_boost_token disetel setelah login via query param")
	}

	// 4. Header Authorization Bearer -> 200
	req4 := httptest.NewRequest("GET", "/api/sites", nil)
	req4.Header.Set("Authorization", "Bearer "+secretToken)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Errorf("diharapkan 200 OK dengan Authorization Bearer, dapat: %d", rec4.Code)
	}

	// 5. Header X-Auth-Token -> 200
	req5 := httptest.NewRequest("GET", "/api/sites", nil)
	req5.Header.Set("X-Auth-Token", secretToken)
	rec5 := httptest.NewRecorder()
	handler.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Errorf("diharapkan 200 OK dengan X-Auth-Token, dapat: %d", rec5.Code)
	}

	// 6. Cookie -> 200
	req6 := httptest.NewRequest("GET", "/api/sites", nil)
	req6.AddCookie(&http.Cookie{Name: "seo_boost_token", Value: secretToken})
	rec6 := httptest.NewRecorder()
	handler.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusOK {
		t.Errorf("diharapkan 200 OK dengan Cookie, dapat: %d", rec6.Code)
	}
}

func TestSSRFProtection(t *testing.T) {
	srv, _ := setupTestServer(t, "", false, nil)
	handler := srv.Handler()

	// Coba akses situs di luar config
	evilSites := []string{"evil.com", "other-site", "localhost", "127.0.0.1"}
	for _, site := range evilSites {
		req := httptest.NewRequest("GET", "/api/sites/"+site+"/pages", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("SSRF gagal dicegah: site %s menghasilkan status %d (diharapkan 404)", site, rec.Code)
		}
	}
}

func TestAPISitesAndPagesAndGaps(t *testing.T) {
	srv, _ := setupTestServer(t, "", false, nil)
	handler := srv.Handler()

	// 1. GET /api/sites
	reqSites := httptest.NewRequest("GET", "/api/sites", nil)
	recSites := httptest.NewRecorder()
	handler.ServeHTTP(recSites, reqSites)
	if recSites.Code != http.StatusOK {
		t.Fatalf("GET /api/sites gagal: %d", recSites.Code)
	}
	var sites []serve.SiteSummary
	if err := json.Unmarshal(recSites.Body.Bytes(), &sites); err != nil {
		t.Fatalf("gagal unmarshal sites: %v", err)
	}
	if len(sites) != 1 || sites[0].Name != "test-site" {
		t.Fatalf("data sites tidak sesuai: %+v", sites)
	}
	if sites[0].TotalPages != 2 || sites[0].IndexedPages != 1 {
		t.Errorf("ringkasan halaman salah: TotalPages=%d, IndexedPages=%d", sites[0].TotalPages, sites[0].IndexedPages)
	}

	// 2. GET /api/sites/test-site/pages
	reqPages := httptest.NewRequest("GET", "/api/sites/test-site/pages", nil)
	recPages := httptest.NewRecorder()
	handler.ServeHTTP(recPages, reqPages)
	if recPages.Code != http.StatusOK {
		t.Fatalf("GET /api/sites/test-site/pages gagal: %d", recPages.Code)
	}
	var pages []*model.Page
	if err := json.Unmarshal(recPages.Body.Bytes(), &pages); err != nil {
		t.Fatalf("gagal unmarshal pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("diharapkan 2 halaman, dapat: %d", len(pages))
	}

	// 3. GET /api/sites/test-site/pages?status=indexed
	reqFiltered := httptest.NewRequest("GET", "/api/sites/test-site/pages?status=indexed", nil)
	recFiltered := httptest.NewRecorder()
	handler.ServeHTTP(recFiltered, reqFiltered)
	var filteredPages []*model.Page
	_ = json.Unmarshal(recFiltered.Body.Bytes(), &filteredPages)
	if len(filteredPages) != 1 || filteredPages[0].URL != "https://example.com/" {
		t.Errorf("filter status=indexed tidak tepat: %+v", filteredPages)
	}

	// 4. GET /api/sites/test-site/gaps
	reqGaps := httptest.NewRequest("GET", "/api/sites/test-site/gaps", nil)
	recGaps := httptest.NewRecorder()
	handler.ServeHTTP(recGaps, reqGaps)
	if recGaps.Code != http.StatusOK {
		t.Fatalf("GET /api/sites/test-site/gaps gagal: %d", recGaps.Code)
	}
	var gaps []model.QueryGap
	if err := json.Unmarshal(recGaps.Body.Bytes(), &gaps); err != nil {
		t.Fatalf("gagal unmarshal gaps: %v", err)
	}
	if len(gaps) != 1 || gaps[0].Query != "test keyword" {
		t.Errorf("gaps tidak cocok: %+v", gaps)
	}

	// 5. GET /api/sites/test-site/runs
	reqRuns := httptest.NewRequest("GET", "/api/sites/test-site/runs", nil)
	recRuns := httptest.NewRecorder()
	handler.ServeHTTP(recRuns, reqRuns)
	if recRuns.Code != http.StatusOK {
		t.Fatalf("GET /api/sites/test-site/runs gagal: %d", recRuns.Code)
	}
	var runs []string
	if err := json.Unmarshal(recRuns.Body.Bytes(), &runs); err != nil {
		t.Fatalf("gagal unmarshal runs: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("diharapkan 1 file run, dapat: %d", len(runs))
	}
}

func TestDashboardHTMLAndSecurityHeaders(t *testing.T) {
	srv, _ := setupTestServer(t, "", false, nil)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / gagal: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type salah: %s", ct)
	}
	if nosniff := rec.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options salah: %s", nosniff)
	}
	if frame := rec.Header().Get("X-Frame-Options"); frame != "DENY" {
		t.Errorf("X-Frame-Options salah: %s", frame)
	}
}

func TestAsyncJobExecutionAndConflict(t *testing.T) {
	runStarted := make(chan struct{})
	runProceed := make(chan struct{})

	mockRunner := func(ctx context.Context, site *config.Site, progress serve.ProgressCallback) error {
		progress("discover", 5, 20, "Menemukan URL sitemap...")
		close(runStarted)
		<-runProceed
		progress("crawl", 20, 20, "Selesai merayapi...")
		return nil
	}

	srv, _ := setupTestServer(t, "", false, mockRunner)
	handler := srv.Handler()

	// 1. Trigger run pertama
	reqTrigger := httptest.NewRequest("POST", "/api/sites/test-site/run", nil)
	recTrigger := httptest.NewRecorder()
	handler.ServeHTTP(recTrigger, reqTrigger)

	if recTrigger.Code != http.StatusAccepted {
		t.Fatalf("diharapkan status 202 Accepted, dapat: %d (%s)", recTrigger.Code, recTrigger.Body.String())
	}

	var triggerResp struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recTrigger.Body.Bytes(), &triggerResp); err != nil {
		t.Fatalf("gagal unmarshal trigger response: %v", err)
	}
	if triggerResp.RunID == "" {
		t.Fatalf("run_id kosong")
	}

	// Tunggu runner aktif
	<-runStarted

	// 2. Trigger run kedua untuk situs yang sama -> 409 Conflict
	reqConflict := httptest.NewRequest("POST", "/api/sites/test-site/run", nil)
	recConflict := httptest.NewRecorder()
	handler.ServeHTTP(recConflict, reqConflict)
	if recConflict.Code != http.StatusConflict {
		t.Errorf("diharapkan 409 Conflict saat run masih aktif, dapat: %d", recConflict.Code)
	}

	// 3. Polling status run saat sedang berjalan
	reqPoll := httptest.NewRequest("GET", "/api/runs/"+triggerResp.RunID, nil)
	recPoll := httptest.NewRecorder()
	handler.ServeHTTP(recPoll, reqPoll)
	if recPoll.Code != http.StatusOK {
		t.Fatalf("GET /api/runs/{id} gagal: %d", recPoll.Code)
	}
	var jobState serve.JobState
	if err := json.Unmarshal(recPoll.Body.Bytes(), &jobState); err != nil {
		t.Fatalf("gagal unmarshal job state: %v", err)
	}
	if jobState.Status != "running" || jobState.Step != "discover" {
		t.Errorf("job state salah saat running: status=%s step=%s", jobState.Status, jobState.Step)
	}

	// 4. Lanjutkan runner sampai selesai
	close(runProceed)

	// Beri jeda kecil untuk goroutine menyelesaikan CompleteJob
	time.Sleep(50 * time.Millisecond)

	recPollDone := httptest.NewRecorder()
	handler.ServeHTTP(recPollDone, reqPoll)
	var finalJobState serve.JobState
	_ = json.Unmarshal(recPollDone.Body.Bytes(), &finalJobState)
	if finalJobState.Status != "completed" {
		t.Errorf("diharapkan job status completed, dapat: %s", finalJobState.Status)
	}
}

func TestConcurrentJobPollRace(t *testing.T) {
	mockRunner := func(ctx context.Context, site *config.Site, progress serve.ProgressCallback) error {
		for i := 1; i <= 10; i++ {
			progress("step", i, 10, "progres update")
			time.Sleep(2 * time.Millisecond)
		}
		return nil
	}

	srv, _ := setupTestServer(t, "", false, mockRunner)
	handler := srv.Handler()

	reqTrigger := httptest.NewRequest("POST", "/api/sites/test-site/run", nil)
	recTrigger := httptest.NewRecorder()
	handler.ServeHTTP(recTrigger, reqTrigger)

	var triggerResp struct {
		RunID string `json:"run_id"`
	}
	_ = json.Unmarshal(recTrigger.Body.Bytes(), &triggerResp)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				req := httptest.NewRequest("GET", "/api/runs/"+triggerResp.RunID, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

func TestMain(m *testing.M) {
	// Setup atau cleanup global bila diperlukan
	code := m.Run()
	_ = os.RemoveAll(filepath.Join(os.TempDir(), "seo_boost_serve_test_*"))
	os.Exit(code)
}
