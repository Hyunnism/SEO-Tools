package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"seo-boost/internal/model"
)

func TestStore_SaveAndLoadPages(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)

	pages := []*model.Page{
		{
			URL:       "https://example.com/",
			Path:      "/",
			InSitemap: true,
			Crawl: &model.CrawlInfo{
				Title:      "Home Page",
				HTTPStatus: 200,
			},
			Index: &model.IndexInfo{
				Status:  model.StatusIndexed,
				Verdict: "PASS",
			},
		},
		{
			URL:       "https://example.com/about",
			Path:      "/about",
			InSitemap: true,
		},
	}

	site := "site1"
	if err := st.SavePages(site, pages); err != nil {
		t.Fatalf("SavePages failed: %v", err)
	}

	loaded, err := st.LoadPages(site)
	if err != nil {
		t.Fatalf("LoadPages failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("loaded pages count = %d, want 2", len(loaded))
	}

	if loaded[0].URL != "https://example.com/" || loaded[0].Crawl.Title != "Home Page" {
		t.Errorf("loaded page 0 data mismatch: %+v", loaded[0])
	}
}

func TestStore_LockConflictAndRelease(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	site := "testsite"

	// 1. Kunci pertama harus berhasil
	unlock1, err := st.Lock(site)
	if err != nil {
		t.Fatalf("lock 1 failed: %v", err)
	}

	// 2. Kunci kedua pada situs yang sama harus gagal
	_, err2 := st.Lock(site)
	if err2 == nil {
		t.Fatal("lock 2 should fail with conflict")
	}

	// 3. Lepas kunci pertama
	unlock1()

	// 4. Kunci ketiga sekarang harus berhasil
	unlock3, err3 := st.Lock(site)
	if err3 != nil {
		t.Fatalf("lock 3 failed after unlock: %v", err3)
	}
	defer unlock3()
}

func TestStore_StaleLockCleanup(t *testing.T) {
	dir := t.TempDir()
	site := "stalesite"
	siteDir := filepath.Join(dir, site)
	_ = os.MkdirAll(siteDir, 0o755)

	lockPath := filepath.Join(siteDir, ".lock")
	_ = os.WriteFile(lockPath, []byte("99999\n2020-01-01T00:00:00Z\n"), 0o600)

	// Set mod time ke 3 jam yang lalu
	oldTime := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(lockPath, oldTime, oldTime)

	st := NewStore(dir)
	unlock, err := st.Lock(site)
	if err != nil {
		t.Fatalf("expected stale lock to be taken over, got error: %v", err)
	}
	defer unlock()
}

func TestStore_RunsHistoryAndPruning(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	site := "runsite"

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Simpan 95 runs
	for i := 1; i <= 95; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		run := &model.RunSummary{
			Timestamp:   ts,
			TotalPages:  10,
			TotalClicks: float64(i * 10),
		}
		if err := st.SaveRun(site, run); err != nil {
			t.Fatalf("SaveRun %d failed: %v", i, err)
		}
	}

	// Periksa jumlah file di runs/: harus dipangkas maksimal 90
	files, err := st.listRunFilesLocked(st.runsDir(site))
	if err != nil {
		t.Fatalf("listRunFilesLocked failed: %v", err)
	}
	if len(files) != 90 {
		t.Fatalf("expected 90 files after pruning, got %d", len(files))
	}

	// Latest run harus run ke-95
	latest, err := st.LoadLatestRun(site)
	if err != nil {
		t.Fatalf("LoadLatestRun failed: %v", err)
	}
	if latest.TotalClicks != 950.0 {
		t.Errorf("latest TotalClicks got %f, want 950.0", latest.TotalClicks)
	}

	// Previous run harus run ke-94
	prev, err := st.LoadPreviousRun(site)
	if err != nil {
		t.Fatalf("LoadPreviousRun failed: %v", err)
	}
	if prev.TotalClicks != 940.0 {
		t.Errorf("previous TotalClicks got %f, want 940.0", prev.TotalClicks)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	site := "concurrent"

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := []*model.Page{
				{URL: fmt.Sprintf("https://example.com/%d", idx)},
			}
			_ = st.SavePages(site, p)
			_, _ = st.LoadPages(site)
		}(i)
	}
	wg.Wait()
}

func TestStore_PruneRunsTo90(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	site := "prune-test"

	baseTime := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// Simpan 95 run berturut-turut
	for i := 1; i <= 95; i++ {
		runTime := baseTime.Add(time.Duration(i) * time.Hour)
		summary := &model.RunSummary{
			TotalPages: i,
			Timestamp:  runTime,
		}
		if err := st.SaveRun(site, summary); err != nil {
			t.Fatalf("gagal simpan run ke-%d: %v", i, err)
		}
	}

	runs, err := st.ListRuns(site)
	if err != nil {
		t.Fatalf("ListRuns gagal: %v", err)
	}

	// Harus dipangkas tepat menjadi 90 berkas
	if len(runs) != 90 {
		t.Fatalf("panjang runs = %d, ingin tepat 90", len(runs))
	}

	// Berkas terlama yang tersisa harus dari run ke-6 (karena 1-5 dipangkas)
	expectedOldestTimestamp := baseTime.Add(6 * time.Hour).Format("2006-01-02T15-04-05Z")
	if !strings.HasPrefix(runs[0], expectedOldestTimestamp) {
		t.Errorf("berkas terlama yang tersisa salah: got %s, want prefix %s", runs[0], expectedOldestTimestamp)
	}

	// Berkas terbaru harus dari run ke-95
	expectedNewestTimestamp := baseTime.Add(95 * time.Hour).Format("2006-01-02T15-04-05Z")
	if !strings.HasPrefix(runs[len(runs)-1], expectedNewestTimestamp) {
		t.Errorf("berkas terbaru salah: got %s, want prefix %s", runs[len(runs)-1], expectedNewestTimestamp)
	}

	// Uji LoadRunByID
	loadedRun, err := st.LoadRunByID(site, runs[0])
	if err != nil {
		t.Fatalf("LoadRunByID gagal: %v", err)
	}
	if loadedRun.TotalPages != 6 {
		t.Errorf("loadedRun.TotalPages = %d, want 6", loadedRun.TotalPages)
	}
}

