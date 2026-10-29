package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"seo-boost/internal/model"
)

func TestComputeDiff(t *testing.T) {
	prev := &model.RunSummary{
		Schema:           1,
		Site:             "awanverse",
		Timestamp:        time.Now().Add(-24 * time.Hour),
		TotalPages:       3,
		IndexedCount:     2,
		TroubledCount:    1,
		Page1Count:       1,
		TotalClicks:      100,
		TotalImpressions: 1000,
		AveragePosition:  8.5,
		PageSummaries: map[string]model.PageStub{
			"https://awanverse.web.id/page1": {
				Status:   model.StatusIndexed,
				Position: 6.0,
				Clicks:   50,
				Page1:    true,
			},
			"https://awanverse.web.id/page2": {
				Status:   model.StatusIndexed,
				Position: 12.0,
				Clicks:   30,
				Page1:    false,
			},
			"https://awanverse.web.id/page3": {
				Status:   model.StatusError,
				Position: 25.0,
				Clicks:   20,
				Page1:    false,
			},
		},
	}

	curr := []*model.Page{
		{
			// page1: drop ranking dari 6.0 ke 10.0 (shift = +4.0 >= 2.0)
			URL: "https://awanverse.web.id/page1",
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
			Perf: &model.PagePerf{
				Clicks:      40,
				Impressions: 800,
				Position:    10.0,
			},
		},
		{
			// page2: deindexed (status error)
			URL: "https://awanverse.web.id/page2",
			Index: &model.IndexInfo{
				Status: model.StatusError,
			},
			Perf: &model.PagePerf{
				Clicks:      10,
				Impressions: 200,
				Position:    12.0,
			},
		},
		{
			// page3: new indexed (sebelumnya error, sekarang indexed), gain ranking dari 25.0 ke 7.0 (shift = -18 <= -2.0)
			URL: "https://awanverse.web.id/page3",
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
			Perf: &model.PagePerf{
				Clicks:      70,
				Impressions: 1500,
				Position:    7.0,
			},
		},
		{
			// page4: halaman baru yang langsung terindeks
			URL: "https://awanverse.web.id/page4",
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
			Perf: &model.PagePerf{
				Clicks:      5,
				Impressions: 100,
				Position:    15.0,
			},
		},
	}

	diff := ComputeDiff(curr, prev)

	if !diff.HasPrevRun {
		t.Fatal("expected diff.HasPrevRun to be true")
	}

	// New indexed: page3 dan page4
	if len(diff.NewIndexed) != 2 {
		t.Errorf("expected 2 new indexed, got %d: %v", len(diff.NewIndexed), diff.NewIndexed)
	}

	// Deindexed: page2
	if len(diff.DeIndexed) != 1 || diff.DeIndexed[0] != "https://awanverse.web.id/page2" {
		t.Errorf("expected [page2] deindexed, got %v", diff.DeIndexed)
	}

	// Rank drops: page1 (shift +4.0)
	if len(diff.RankDrops) != 1 || diff.RankDrops[0].URL != "https://awanverse.web.id/page1" {
		t.Errorf("expected 1 rank drop on page1, got %v", diff.RankDrops)
	}

	// Rank gains: page3 (shift -18.0)
	if len(diff.RankGains) != 1 || diff.RankGains[0].URL != "https://awanverse.web.id/page3" {
		t.Errorf("expected 1 rank gain on page3, got %v", diff.RankGains)
	}

	// Deltas
	expectedDeltaClicks := (40.0 + 10.0 + 70.0 + 5.0) - 100.0 // 125 - 100 = 25
	if diff.DeltaClicks != expectedDeltaClicks {
		t.Errorf("expected delta clicks %.1f, got %.1f", expectedDeltaClicks, diff.DeltaClicks)
	}

	if !diff.HasMeaningfulChanges() {
		t.Errorf("expected HasMeaningfulChanges to be true")
	}

	emptyDiff := ComputeDiff(nil, nil)
	if emptyDiff.HasMeaningfulChanges() {
		t.Errorf("expected empty diff HasMeaningfulChanges to be false")
	}
}

func TestRenderText_NoEmojiOrUnicode(t *testing.T) {
	data := &ReportData{
		Site:        "awanverse",
		GSCProperty: "https://awanverse.web.id/",
		Timestamp:   time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		Summary: &model.RunSummary{
			TotalPages:       10,
			IndexedCount:     8,
			TroubledCount:    2,
			Page1Count:       4,
			TotalClicks:      500,
			TotalImpressions: 5000,
			AveragePosition:  7.2,
		},
		Diff: &RunDiff{
			HasPrevRun:       true,
			NewIndexed:       []string{"https://awanverse.web.id/new-article"},
			DeIndexed:        []string{"https://awanverse.web.id/broken"},
			RankGains:        []RankShift{{URL: "https://awanverse.web.id/winner", OldPosition: 9.0, NewPosition: 3.0, Shift: -6.0}},
			RankDrops:        []RankShift{{URL: "https://awanverse.web.id/loser", OldPosition: 4.0, NewPosition: 8.5, Shift: 4.5}},
			DeltaClicks:      50,
			DeltaImpressions: 500,
			DeltaAvgPosition: -1.2,
		},
		TopGaps: []model.QueryGap{
			{
				QueryPerf: model.QueryPerf{Query: "belajar golang", Position: 6.2, Impressions: 1200, CTR: 0.04},
				Category:  model.CategoryLowHangingFruit,
				Match:     model.MatchPartial,
				Score:     14.5,
			},
		},
		AuditIssues: []ReportedIssue{
			{
				URL:      "https://awanverse.web.id/broken",
				Code:     "HTTP_500",
				Detail:   "Server error returned",
				Severity: "error",
			},
		},
		Pages: []*model.Page{
			{
				URL: "https://awanverse.web.id/winner",
				Index: &model.IndexInfo{
					Status: model.StatusIndexed,
				},
				Perf: &model.PagePerf{
					Clicks:      200,
					Impressions: 1500,
					Position:    3.0,
				},
			},
		},
	}

	var buf bytes.Buffer
	err := RenderText(&buf, data)
	if err != nil {
		t.Fatalf("unexpected error rendering text: %v", err)
	}

	out := buf.String()

	// 1. Verify necessary badges exist
	requiredBadges := []string{
		"[OK]", "[WARN]", "[ERROR]", "[P1]", "[BOOST]",
		"[+]", "[-]", "[UP]", "[DOWN]",
	}
	for _, badge := range requiredBadges {
		if !strings.Contains(out, badge) {
			t.Errorf("expected output to contain badge %q", badge)
		}
	}

	// 2. Strict check: verify NO emoji or non-ASCII decorative characters exist
	for i, r := range out {
		// allow standard ascii whitespace, printables
		if r > unicode.MaxASCII {
			t.Errorf("forbidden non-ASCII rune %q (U+%04X) found at offset %d. User rule prohibits emoji/decorative unicode.", r, r, i)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	data := &ReportData{
		Site:      "awanverse",
		Timestamp: time.Now(),
		Summary: &model.RunSummary{
			TotalPages:   5,
			IndexedCount: 4,
		},
	}

	var buf bytes.Buffer
	err := RenderJSON(&buf, data)
	if err != nil {
		t.Fatalf("unexpected error rendering json: %v", err)
	}

	var env JSONEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal rendered json: %v", err)
	}

	if env.Schema != 1 {
		t.Errorf("expected schema 1, got %d", env.Schema)
	}
	if env.Data.Site != "awanverse" {
		t.Errorf("expected site 'awanverse', got %q", env.Data.Site)
	}
}

func TestRenderMarkdown_GoldenStructure(t *testing.T) {
	data := &ReportData{
		Site:        "example",
		GSCProperty: "sc-domain:example.com",
		Timestamp:   time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		Summary: &model.RunSummary{
			TotalPages:       5,
			IndexedCount:     4,
			TroubledCount:    1,
			Page1Count:       2,
			TotalClicks:      120,
			TotalImpressions: 1500,
			AveragePosition:  5.4,
		},
		Diff: &RunDiff{
			HasPrevRun:       true,
			NewIndexed:       []string{"https://example.com/new"},
			DeIndexed:        []string{"https://example.com/gone"},
			RankGains:        []RankShift{{URL: "https://example.com/gain", OldPosition: 8.0, NewPosition: 4.0, Shift: -4.0}},
			RankDrops:        []RankShift{{URL: "https://example.com/drop", OldPosition: 3.0, NewPosition: 7.0, Shift: 4.0}},
			DeltaClicks:      20,
			DeltaImpressions: 300,
			DeltaAvgPosition: -0.5,
		},
		TopGaps: []model.QueryGap{
			{
				QueryPerf: model.QueryPerf{Query: "test keyword", Position: 4.2, Impressions: 800, CTR: 0.05},
				Category:  model.CategoryLowHangingFruit,
				Match:     model.MatchPartial,
				Score:     18.2,
			},
		},
		AuditIssues: []ReportedIssue{
			{
				URL:      "https://example.com/drop",
				Code:     "MISSING_H1",
				Severity: "error",
				Detail:   "H1 tag not found",
			},
		},
		Pages: []*model.Page{
			{
				URL:  "https://example.com/gain",
				Path: "/gain",
				Crawl: &model.CrawlInfo{
					HTTPStatus: 200,
					Title:      "Gain Page",
				},
				Index: &model.IndexInfo{
					Status: model.StatusIndexed,
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, data); err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "# SEO Report: example") {
		t.Errorf("missing title in markdown: %s", out)
	}
	if !strings.Contains(out, "| Terindeks [OK] | 4 |") {
		t.Errorf("missing summary row in markdown: %s", out)
	}
	if !strings.Contains(out, "### [+] Halaman Baru Terindeks (1)") {
		t.Errorf("missing new indexed header: %s", out)
	}
	if !strings.Contains(out, "### [UP] Kenaikan Peringkat Signifikan (>= 3 Posisi, Total: 1)") {
		t.Errorf("missing rank gain header: %s", out)
	}

	// Verifikasi tidak ada karakter emoji atau non-ASCII
	for i, r := range out {
		if r > unicode.MaxASCII {
			t.Errorf("forbidden non-ASCII character %q at offset %d in markdown", r, i)
		}
	}
}

func TestRenderHTML_XSS_AutoEscaping(t *testing.T) {
	data := &ReportData{
		Site:        "xss-test",
		Timestamp:   time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		Summary: &model.RunSummary{
			TotalPages:   1,
			IndexedCount: 1,
		},
		TopGaps: []model.QueryGap{
			{
				QueryPerf: model.QueryPerf{Query: "<script>alert('xss-query')</script>"},
				Category:  "BOOST",
				Match:     model.MatchAbsent,
				Score:     10.0,
			},
		},
		AuditIssues: []ReportedIssue{
			{
				URL:      "https://example.com/issue",
				Code:     "TEST",
				Severity: "error",
				Detail:   "<img src=x onerror=alert('xss-detail')>",
			},
		},
		Pages: []*model.Page{
			{
				URL: "https://example.com/malicious",
				Crawl: &model.CrawlInfo{
					HTTPStatus: 200,
					Title:      "<script>alert('xss-title')</script>",
				},
				Index: &model.IndexInfo{
					Status: model.StatusIndexed,
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := RenderHTML(&buf, data); err != nil {
		t.Fatalf("RenderHTML failed: %v", err)
	}

	out := buf.String()

	// Pastikan HTML template meng-escape string XSS secara otomatis
	if strings.Contains(out, "<script>alert('xss-title')</script>") {
		t.Errorf("XSS unescaped in title: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(&#39;xss-title&#39;)&lt;/script&gt;") && !strings.Contains(out, "&lt;script&gt;alert('xss-title')&lt;/script&gt;") {
		t.Errorf("expected escaped title in HTML, got: %s", out)
	}

	if strings.Contains(out, "<script>alert('xss-query')</script>") {
		t.Errorf("XSS unescaped in query: %s", out)
	}
	if strings.Contains(out, "<img src=x onerror=alert('xss-detail')>") {
		t.Errorf("XSS unescaped in detail: %s", out)
	}
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Errorf("missing DOCTYPE in HTML: %s", out)
	}
}

func TestHasCriticalIssues(t *testing.T) {
	// 1. Kasus Bersih
	cleanPages := []*model.Page{
		{
			URL:  "https://example.com/",
			Path: "/",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 200,
			},
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
		},
	}
	if HasCriticalIssues(cleanPages, nil, []string{"/"}) {
		t.Errorf("expected no critical issues for clean pages")
	}

	// 2. Kasus Isu Audit dengan Severity Error
	errorIssues := []ReportedIssue{
		{Code: "ERR_CANONICAL", Severity: "error", Detail: "Invalid canonical"},
	}
	if !HasCriticalIssues(cleanPages, errorIssues, []string{"/"}) {
		t.Errorf("expected critical issue when error severity present")
	}

	// 3. Kasus Halaman HTTP 500
	brokenPages := []*model.Page{
		{
			URL:  "https://example.com/broken",
			Path: "/broken",
			Crawl: &model.CrawlInfo{
				HTTPStatus: 500,
			},
		},
	}
	if !HasCriticalIssues(brokenPages, nil, nil) {
		t.Errorf("expected critical issue when HTTP status 500 present")
	}

	// 4. Kasus Halaman StatusBlocked
	blockedPages := []*model.Page{
		{
			URL:  "https://example.com/blocked",
			Path: "/blocked",
			Index: &model.IndexInfo{
				Status: model.StatusBlocked,
			},
		},
	}
	if !HasCriticalIssues(blockedPages, nil, nil) {
		t.Errorf("expected critical issue when status blocked present")
	}

	// 5. Kasus Halaman Prioritas Hilang/Tidak Terindeks
	unindexedPriority := []*model.Page{
		{
			URL:  "https://example.com/vip",
			Path: "/vip",
			Index: &model.IndexInfo{
				Status: model.StatusCrawledNotIndexed,
			},
		},
	}
	if !HasCriticalIssues(unindexedPriority, nil, []string{"/vip"}) {
		t.Errorf("expected critical issue when priority path is not indexed")
	}
}
