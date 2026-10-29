package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"seo-boost/internal/model"
)

// RenderMarkdown menuliskan laporan SEO dalam format GitHub Flavored Markdown (GFM) bersih tanpa karakter emoji.
func RenderMarkdown(w io.Writer, data *ReportData) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# SEO Report: %s\n\n", data.Site))
	sb.WriteString(fmt.Sprintf("- **Waktu Analisis**: %s\n", data.Timestamp.Format("2006-01-02 15:04:05 MST")))
	if data.GSCProperty != "" {
		sb.WriteString(fmt.Sprintf("- **GSC Property**: `%s`\n", data.GSCProperty))
	}
	sb.WriteString("\n---\n\n")

	// 1. Ringkasan Eksekutif
	if data.Summary != nil {
		sb.WriteString("## 1. Ringkasan Eksekutif\n\n")
		sb.WriteString("| Metrik | Nilai |\n")
		sb.WriteString("|---|---|\n")
		sb.WriteString(fmt.Sprintf("| Total Halaman | %d |\n", data.Summary.TotalPages))
		sb.WriteString(fmt.Sprintf("| Terindeks [OK] | %d |\n", data.Summary.IndexedCount))
		sb.WriteString(fmt.Sprintf("| Bermasalah [WARN] | %d |\n", data.Summary.TroubledCount))
		sb.WriteString(fmt.Sprintf("| Halaman 1 Google [P1] | %d |\n", data.Summary.Page1Count))
		sb.WriteString(fmt.Sprintf("| Total Clicks | %.0f |\n", data.Summary.TotalClicks))
		sb.WriteString(fmt.Sprintf("| Total Impressions | %.0f |\n", data.Summary.TotalImpressions))
		if data.Summary.AveragePosition > 0 {
			sb.WriteString(fmt.Sprintf("| Rata-rata Posisi | %.2f |\n", data.Summary.AveragePosition))
		}
		sb.WriteString("\n")
	}

	// 2. Perubahan Status & Peringkat (Diff)
	if data.Diff != nil && data.Diff.HasPrevRun {
		sb.WriteString("## 2. Perubahan vs Run Sebelumnya (Diff)\n\n")
		sb.WriteString("| Metrik Delta | Perubahan |\n")
		sb.WriteString("|---|---|\n")
		sb.WriteString(fmt.Sprintf("| Delta Clicks | %+.0f |\n", data.Diff.DeltaClicks))
		sb.WriteString(fmt.Sprintf("| Delta Impressions | %+.0f |\n", data.Diff.DeltaImpressions))
		if data.Diff.DeltaAvgPosition != 0 {
			sb.WriteString(fmt.Sprintf("| Delta Avg Position | %+.2f (negatif = membaik) |\n", data.Diff.DeltaAvgPosition))
		}
		sb.WriteString("\n")

		if len(data.Diff.NewIndexed) > 0 {
			sb.WriteString(fmt.Sprintf("### [+] Halaman Baru Terindeks (%d)\n\n", len(data.Diff.NewIndexed)))
			for _, u := range data.Diff.NewIndexed {
				sb.WriteString(fmt.Sprintf("- `%s`\n", u))
			}
			sb.WriteString("\n")
		}

		if len(data.Diff.DeIndexed) > 0 {
			sb.WriteString(fmt.Sprintf("### [-] Halaman Hilang dari Indeks (%d)\n\n", len(data.Diff.DeIndexed)))
			for _, u := range data.Diff.DeIndexed {
				sb.WriteString(fmt.Sprintf("- `%s`\n", u))
			}
			sb.WriteString("\n")
		}

		if len(data.Diff.RankGains) > 0 {
			sb.WriteString(fmt.Sprintf("### [UP] Kenaikan Peringkat Signifikan (>= 3 Posisi, Total: %d)\n\n", len(data.Diff.RankGains)))
			sb.WriteString("| URL | Posisi Lama | Posisi Baru | Kenaikan |\n")
			sb.WriteString("|---|---|---|---|\n")
			for _, r := range data.Diff.RankGains {
				sb.WriteString(fmt.Sprintf("| `%s` | %.1f | %.1f | %+.1f |\n", r.URL, r.OldPosition, r.NewPosition, -r.Shift))
			}
			sb.WriteString("\n")
		}

		if len(data.Diff.RankDrops) > 0 {
			sb.WriteString(fmt.Sprintf("### [DOWN] Penurunan Peringkat Signifikan (>= 3 Posisi, Total: %d)\n\n", len(data.Diff.RankDrops)))
			sb.WriteString("| URL | Posisi Lama | Posisi Baru | Penurunan |\n")
			sb.WriteString("|---|---|---|---|\n")
			for _, r := range data.Diff.RankDrops {
				sb.WriteString(fmt.Sprintf("| `%s` | %.1f | %.1f | %+.1f |\n", r.URL, r.OldPosition, r.NewPosition, r.Shift))
			}
			sb.WriteString("\n")
		}
	}

	// 3. Peluang Keyword Gap Teratas
	if len(data.TopGaps) > 0 {
		sb.WriteString("## 3. Peluang Keyword Gap Teratas\n\n")
		sb.WriteString("| Skor | Tipe | Query | Posisi | Impresi | CTR | Match |\n")
		sb.WriteString("|---|---|---|---|---|---|---|\n")
		for _, g := range data.TopGaps {
			badge := fmt.Sprintf("[%s]", g.Category)
			sb.WriteString(fmt.Sprintf("| %.1f | %s | %s | %.1f | %.0f | %.1f%% | %s |\n",
				g.Score, badge, g.Query, g.Position, g.Impressions, g.CTR*100, g.Match))
		}
		sb.WriteString("\n")
	}

	// 4. Audit On-Page SEO
	if len(data.AuditIssues) > 0 {
		sb.WriteString("## 4. Temuan Audit On-Page\n\n")
		sb.WriteString("| Tingkat | Kode | Keterangan | URL |\n")
		sb.WriteString("|---|---|---|---|\n")
		for _, iss := range data.AuditIssues {
			badge := fmt.Sprintf("[%s]", strings.ToUpper(iss.Severity))
			urlStr := iss.URL
			if urlStr == "" {
				urlStr = "-"
			} else {
				urlStr = fmt.Sprintf("`%s`", urlStr)
			}
			sb.WriteString(fmt.Sprintf("| %s | `%s` | %s | %s |\n", badge, iss.Code, iss.Detail, urlStr))
		}
		sb.WriteString("\n")
	}

	// 5. Daftar Status Halaman
	if len(data.Pages) > 0 {
		sortedPages := make([]*model.Page, len(data.Pages))
		copy(sortedPages, data.Pages)
		sort.Slice(sortedPages, func(i, j int) bool {
			// Prioritaskan status bermasalah di atas
			iTroubled := isTroubledPage(sortedPages[i])
			jTroubled := isTroubledPage(sortedPages[j])
			if iTroubled != jTroubled {
				return iTroubled
			}
			return sortedPages[i].URL < sortedPages[j].URL
		})

		sb.WriteString("## 5. Rincian Halaman\n\n")
		sb.WriteString("| Status Index | HTTP | URL | Judul | Clicks | Impresi | Posisi |\n")
		sb.WriteString("|---|---|---|---|---|---|---|\n")
		for _, p := range sortedPages {
			idxStatus := "[UNINSPECTED]"
			if p.Index != nil {
				idxStatus = string(p.Index.Status)
			}
			httpCode := 0
			title := "-"
			if p.Crawl != nil {
				httpCode = p.Crawl.HTTPStatus
				if p.Crawl.Title != "" {
					title = strings.ReplaceAll(p.Crawl.Title, "|", "/")
				}
			}
			clicks := 0.0
			imp := 0.0
			pos := 0.0
			if p.Perf != nil {
				clicks = p.Perf.Clicks
				imp = p.Perf.Impressions
				pos = p.Perf.Position
			}
			sb.WriteString(fmt.Sprintf("| %s | %d | `%s` | %s | %.0f | %.0f | %.1f |\n",
				idxStatus, httpCode, p.URL, title, clicks, imp, pos))
		}
		sb.WriteString("\n")
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

func isTroubledPage(p *model.Page) bool {
	if p == nil {
		return false
	}
	if p.Crawl != nil && p.Crawl.HTTPStatus != 0 && p.Crawl.HTTPStatus != 200 {
		return true
	}
	if p.Index != nil {
		switch p.Index.Status {
		case model.StatusBlocked, model.StatusCrawledNotIndexed, model.StatusDiscoveredNotIndexed:
			return true
		}
	}
	for _, iss := range p.Audit {
		if strings.EqualFold(iss.Severity, "error") {
			return true
		}
	}
	return false
}
