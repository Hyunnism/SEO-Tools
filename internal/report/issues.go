package report

import (
	"net/http"
	"strings"

	"seo-boost/internal/model"
)

// HasCriticalIssues mengevaluasi apakah ada masalah SEO berbobot kritis pada hasil run.
// Kriteria masalah kritis mencakup:
// 1. Isu audit on-page dengan severity "error".
// 2. Halaman dengan respons HTTP error (bukan 200) atau berstatus Google StatusBlocked.
// 3. Halaman yang berada di priorityPaths konfigurasi tetapi tidak terindeks oleh Google.
func HasCriticalIssues(pages []*model.Page, issues []ReportedIssue, priorityPaths []string) bool {
	// 1. Cek isu audit dengan severity "error"
	for _, iss := range issues {
		if strings.EqualFold(iss.Severity, "error") {
			return true
		}
	}

	for _, p := range pages {
		if p == nil {
			continue
		}

		// 2. Cek status HTTP error atau status Google blocked
		if p.Crawl != nil && p.Crawl.HTTPStatus != 0 && p.Crawl.HTTPStatus != http.StatusOK {
			return true
		}
		if p.Index != nil && p.Index.Status == model.StatusBlocked {
			return true
		}

		// 3. Cek halaman prioritas yang tidak terindeks
		for _, pref := range priorityPaths {
			if strings.HasPrefix(p.Path, pref) {
				if p.Index != nil && p.Index.Status != model.StatusIndexed && p.Index.Status != model.StatusNeverInspected {
					return true
				}
			}
		}
	}

	return false
}
