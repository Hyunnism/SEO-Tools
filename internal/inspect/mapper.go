package inspect

import (
	"strings"
	"time"

	"seo-boost/internal/google"
	"seo-boost/internal/model"
)

var errorPageFetchStates = map[string]bool{
	"NOT_FOUND":             true,
	"SOFT_404":              true,
	"SERVER_ERROR":          true,
	"REDIRECT_ERROR":        true,
	"ACCESS_DENIED":         true,
	"ACCESS_FORBIDDEN":      true,
	"BLOCKED_4XX":           true,
	"INTERNAL_CRAWL_ERROR":  true,
	"INVALID_URL":           true,
}

// MapIndexStatus memetakan respons IndexStatusResult dari GSC ke model.IndexInfo
// dengan urutan prioritas ketat sesuai spesifikasi blueprint (bagian 9.4).
func MapIndexStatus(raw *google.IndexStatusResult) *model.IndexInfo {
	if raw == nil {
		return &model.IndexInfo{
			InspectedAt: time.Now(),
			Status:      model.StatusNeverInspected,
		}
	}

	var lastCrawl *time.Time
	if raw.LastCrawlTime != "" {
		if t, err := time.Parse(time.RFC3339, raw.LastCrawlTime); err == nil {
			lastCrawl = &t
		}
	}

	info := &model.IndexInfo{
		InspectedAt:   time.Now(),
		Verdict:       raw.Verdict,
		RawCoverage:   raw.CoverageState,
		LastCrawlTime: lastCrawl,
		CrawledAs:     raw.CrawledAs,
		PageFetch:     raw.PageFetchState,
		RobotsTxt:     raw.RobotsTxtState,
		IndexingState: raw.IndexingState,
		GoogleCanon:   raw.GoogleCanonical,
		UserCanon:     raw.UserCanonical,
	}

	info.Status = determineStatus(raw)
	return info
}

func determineStatus(raw *google.IndexStatusResult) model.IndexStatus {
	// 1. Cek Blokir robots.txt atau indexingState BLOCKED_*
	if strings.EqualFold(raw.RobotsTxtState, "DISALLOWED") ||
		strings.Contains(strings.ToUpper(raw.IndexingState), "BLOCKED_") {
		return model.StatusBlocked
	}

	// 2. Cek pageFetchState yang mengindikasikan error
	if errorPageFetchStates[strings.ToUpper(raw.PageFetchState)] {
		return model.StatusError
	}

	// 3. Cek verdict == "PASS"
	if strings.EqualFold(raw.Verdict, "PASS") {
		return model.StatusIndexed
	}

	// 4-7. Cek coverageState (case-insensitive substring check)
	cov := strings.ToLower(raw.CoverageState)
	if strings.Contains(cov, "crawled - currently not indexed") {
		return model.StatusCrawledNotIndexed
	}
	if strings.Contains(cov, "discovered - currently not indexed") {
		return model.StatusDiscoveredNotIndexed
	}
	if strings.Contains(cov, "unknown to google") {
		return model.StatusUnknownToGoogle
	}
	if strings.Contains(cov, "duplicate") || strings.Contains(cov, "canonical") {
		return model.StatusDuplicate
	}

	// 8. Fallback: jika tidak ada yang cocok, gunakan status "other"
	return model.StatusOther
}
