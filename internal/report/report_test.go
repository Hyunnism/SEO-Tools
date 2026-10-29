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
