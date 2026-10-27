package perf

import (
	"math"
	"testing"
	"time"

	"seo-boost/internal/google"
)

func TestCalculateDateRange(t *testing.T) {
	// 2026-10-27
	today := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	start, end := CalculateDateRange(today, 28)

	// endDate = 2026-10-27 - 3 hari = 2026-10-24
	if end != "2026-10-24" {
		t.Errorf("got end date %s, want 2026-10-24", end)
	}

	// 28 hari sebelum 2026-10-24 inklusif = 2026-09-27
	if start != "2026-09-27" {
		t.Errorf("got start date %s, want 2026-09-27", start)
	}
}

func TestAggregateRows(t *testing.T) {
	baseURL := "https://example.com"
	agg := NewAggregator(nil, baseURL)

	rows := []google.SearchAnalyticsRow{
		{
			Keys:        []string{"https://example.com/blog", "belajar seo go"},
			Clicks:      20,
			Impressions: 100,
			CTR:         0.20,
			Position:    2.0,
		},
		{
			Keys:        []string{"https://example.com/blog", "tutorial webmasters api"},
			Clicks:      10,
			Impressions: 300,
			CTR:         0.033,
			Position:    8.0,
		},
		{
			Keys:        []string{"https://example.com/about", "kontak saya"},
			Clicks:      5,
			Impressions: 50,
			CTR:         0.10,
			Position:    10.0,
		},
		{
			Keys:        []string{"https://example.com/service", "jasa konsultan"},
			Clicks:      1,
			Impressions: 10,
			CTR:         0.10,
			Position:    10.1, // sedikit di luar halaman 1
		},
	}

	res := agg.AggregateRows(rows)

	if len(res) != 3 {
		t.Fatalf("expected 3 aggregated pages, got %d", len(res))
	}

	// 1. Periksa /blog
	blogPerf, found := res["https://example.com/blog"]
	if !found {
		t.Fatal("https://example.com/blog not found in result")
	}

	// Clicks: 20 + 10 = 30
	if blogPerf.Clicks != 30 {
		t.Errorf("blog clicks got %f, want 30", blogPerf.Clicks)
	}
	// Impressions: 100 + 300 = 400
	if blogPerf.Impressions != 400 {
		t.Errorf("blog impressions got %f, want 400", blogPerf.Impressions)
	}
	// CTR = 30 / 400 = 0.075
	if math.Abs(blogPerf.CTR-0.075) > 1e-6 {
		t.Errorf("blog CTR got %f, want 0.075", blogPerf.CTR)
	}
	// Weighted position: (2.0 * 100 + 8.0 * 300) / 400 = (200 + 2400) / 400 = 2600 / 400 = 6.5
	if math.Abs(blogPerf.Position-6.5) > 1e-6 {
		t.Errorf("blog position got %f, want 6.5", blogPerf.Position)
	}
	// Page 1 (6.5 <= 10.0) -> true
	if !blogPerf.Page1 {
		t.Error("blog should be on Page1 (pos 6.5 <= 10.0)")
	}
	// Queries sorted by clicks desc: "belajar seo go" (20 clicks) first, "tutorial webmasters api" (10 clicks) second
	if len(blogPerf.Queries) != 2 || blogPerf.Queries[0].Query != "belajar seo go" {
		t.Errorf("blog queries order invalid: %+v", blogPerf.Queries)
	}

	// 2. Periksa /about (posisi tepat 10.0 -> Page1 harus true)
	aboutPerf := res["https://example.com/about"]
	if !aboutPerf.Page1 {
		t.Errorf("about position 10.0 should be Page1 true, got %v", aboutPerf.Page1)
	}

	// 3. Periksa /service (posisi 10.1 -> Page1 harus false)
	svcPerf := res["https://example.com/service"]
	if svcPerf.Page1 {
		t.Errorf("service position 10.1 should be Page1 false, got %v", svcPerf.Page1)
	}
}
