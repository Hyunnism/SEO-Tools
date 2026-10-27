package inspect

import (
	"testing"

	"seo-boost/internal/google"
	"seo-boost/internal/model"
)

func TestMapIndexStatus_TableCases(t *testing.T) {
	tests := []struct {
		name       string
		raw        *google.IndexStatusResult
		wantStatus model.IndexStatus
	}{
		{
			name: "1. Blocked via robots.txt",
			raw: &google.IndexStatusResult{
				RobotsTxtState: "DISALLOWED",
				Verdict:        "FAIL",
			},
			wantStatus: model.StatusBlocked,
		},
		{
			name: "1b. Blocked via indexingState",
			raw: &google.IndexStatusResult{
				IndexingState: "BLOCKED_BY_META_TAG",
				Verdict:       "FAIL",
			},
			wantStatus: model.StatusBlocked,
		},
		{
			name: "2. PageFetchState NOT_FOUND",
			raw: &google.IndexStatusResult{
				PageFetchState: "NOT_FOUND",
				Verdict:        "FAIL",
			},
			wantStatus: model.StatusError,
		},
		{
			name: "2b. PageFetchState SERVER_ERROR",
			raw: &google.IndexStatusResult{
				PageFetchState: "SERVER_ERROR",
				Verdict:        "FAIL",
			},
			wantStatus: model.StatusError,
		},
		{
			name: "2c. PageFetchState SOFT_404",
			raw: &google.IndexStatusResult{
				PageFetchState: "SOFT_404",
				Verdict:        "FAIL",
			},
			wantStatus: model.StatusError,
		},
		{
			name: "3. Verdict PASS (Indexed)",
			raw: &google.IndexStatusResult{
				Verdict:       "PASS",
				CoverageState: "Submitted and indexed",
			},
			wantStatus: model.StatusIndexed,
		},
		{
			name: "4. Crawled - currently not indexed",
			raw: &google.IndexStatusResult{
				Verdict:       "NEUTRAL",
				CoverageState: "Crawled - currently not indexed",
			},
			wantStatus: model.StatusCrawledNotIndexed,
		},
		{
			name: "5. Discovered - currently not indexed",
			raw: &google.IndexStatusResult{
				Verdict:       "NEUTRAL",
				CoverageState: "Discovered - currently not indexed",
			},
			wantStatus: model.StatusDiscoveredNotIndexed,
		},
		{
			name: "6. Unknown to Google",
			raw: &google.IndexStatusResult{
				Verdict:       "NEUTRAL",
				CoverageState: "URL is unknown to Google",
			},
			wantStatus: model.StatusUnknownToGoogle,
		},
		{
			name: "7. Duplicate / canonical",
			raw: &google.IndexStatusResult{
				Verdict:       "NEUTRAL",
				CoverageState: "Duplicate, Google chose different canonical than user",
			},
			wantStatus: model.StatusDuplicate,
		},
		{
			name: "8. Unrecognized coverageState -> other",
			raw: &google.IndexStatusResult{
				Verdict:       "NEUTRAL",
				CoverageState: "Some newly introduced Google status wording 2026",
			},
			wantStatus: model.StatusOther,
		},
		{
			name:       "Nil raw result -> never_inspected",
			raw:        nil,
			wantStatus: model.StatusNeverInspected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := MapIndexStatus(tt.raw)
			if info.Status != tt.wantStatus {
				t.Errorf("got status %q, want %q", info.Status, tt.wantStatus)
			}
			if tt.raw != nil && info.RawCoverage != tt.raw.CoverageState {
				t.Errorf("RawCoverage not preserved: got %q, want %q", info.RawCoverage, tt.raw.CoverageState)
			}
		})
	}
}
