package inspect

import (
	"testing"
	"time"
)

func TestQuotaManager_PacificTimeAndReset(t *testing.T) {
	dir := t.TempDir()

	// 2026-10-25 23:59:00 Pacific Time (UTC-7 atau UTC-8)
	loc, _ := time.LoadLocation("America/Los_Angeles")
	if loc == nil {
		loc = time.FixedZone("PST", -8*3600)
	}

	simulatedTime := time.Date(2026, 10, 25, 23, 59, 0, 0, loc)
	clock := func() time.Time {
		return simulatedTime
	}

	qm := NewQuotaManager(dir, clock)

	// Hari pertama (2026-10-25): increment 150 request
	if err := qm.Increment("site1", 150); err != nil {
		t.Fatalf("Increment failed: %v", err)
	}

	used, err := qm.GetUsedToday("site1")
	if err != nil {
		t.Fatalf("GetUsedToday failed: %v", err)
	}
	if used != 150 {
		t.Errorf("got used %d, want 150", used)
	}

	budget := 200
	if rem := qm.RemainingBudget("site1", budget); rem != 50 {
		t.Errorf("got remaining %d, want 50", rem)
	}

	// Majukan waktu 2 menit melewati batas tengah malam Pacific -> 2026-10-26 00:01:00
	simulatedTime = time.Date(2026, 10, 26, 0, 1, 0, 0, loc)

	// Kuota harus otomatis reset ke 0 untuk hari baru
	usedNextDay, err := qm.GetUsedToday("site1")
	if err != nil {
		t.Fatalf("GetUsedToday on next day failed: %v", err)
	}
	if usedNextDay != 0 {
		t.Errorf("expected quota to reset to 0 on new Pacific day, got %d", usedNextDay)
	}
	if rem := qm.RemainingBudget("site1", budget); rem != 200 {
		t.Errorf("got remaining on next day %d, want 200", rem)
	}

	// Tambah pemakaian di hari baru
	if err := qm.Increment("site1", 20); err != nil {
		t.Fatalf("Increment on next day failed: %v", err)
	}
	usedNextDay, _ = qm.GetUsedToday("site1")
	if usedNextDay != 20 {
		t.Errorf("got used %d, want 20", usedNextDay)
	}
}
