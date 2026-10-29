package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"seo-boost/internal/model"
)

// ReportedIssue menyimpan temuan audit yang dikaitkan dengan URL (jika ada).
type ReportedIssue struct {
	URL      string `json:"url,omitempty"`
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warn | info
	Detail   string `json:"detail"`
}

// ReportData berisi data komprehensif untuk dirender dalam laporan.
type ReportData struct {
	Site        string              `json:"site"`
	GSCProperty string              `json:"gsc_property,omitempty"`
	Timestamp   time.Time           `json:"timestamp"`
	Summary     *model.RunSummary   `json:"summary"`
	Diff        *RunDiff            `json:"diff,omitempty"`
	TopGaps     []model.QueryGap    `json:"top_gaps,omitempty"`
	AuditIssues []ReportedIssue     `json:"audit_issues,omitempty"`
	Pages       []*model.Page       `json:"pages,omitempty"`
}

// JSONEnvelope mendefinisikan pembungkus versioning standar untuk export JSON.
type JSONEnvelope struct {
	Schema int         `json:"schema"`
	Data   *ReportData `json:"data"`
}

// RenderJSON menulis report ke w dalam format JSON terstruktur dengan schema version.
func RenderJSON(w io.Writer, data *ReportData) error {
	envelope := JSONEnvelope{
		Schema: 1,
		Data:   data,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

// RenderText menulis report ke w dalam format teks bersih untuk terminal.
// Sesuai aturan: tidak ada emoji atau decorative unicode, gunakan badge teks informatif.
func RenderText(w io.Writer, data *ReportData) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(w, "================================================================================\n")
	fmt.Fprintf(w, "  SEO BOOST REPORT: %s\n", data.Site)
	fmt.Fprintf(w, "  Waktu: %s\n", data.Timestamp.Format("2006-01-02 15:04:05 MST"))
	if data.GSCProperty != "" {
		fmt.Fprintf(w, "  GSC Property: %s\n", data.GSCProperty)
	}
	fmt.Fprintf(w, "================================================================================\n\n")

	// 1. Ringkasan Eksekutif
	if data.Summary != nil {
		fmt.Fprintf(w, "--- RINGKASAN SITUS ---\n")
		fmt.Fprintf(tw, "Total Halaman:\t%d\n", data.Summary.TotalPages)
		fmt.Fprintf(tw, "Terindeks [OK]:\t%d\n", data.Summary.IndexedCount)
		fmt.Fprintf(tw, "Bermasalah [WARN]:\t%d\n", data.Summary.TroubledCount)
		fmt.Fprintf(tw, "Halaman 1 Google [P1]:\t%d\n", data.Summary.Page1Count)
		fmt.Fprintf(tw, "Total Clicks:\t%.0f\n", data.Summary.TotalClicks)
		fmt.Fprintf(tw, "Total Impressions:\t%.0f\n", data.Summary.TotalImpressions)
		if data.Summary.AveragePosition > 0 {
			fmt.Fprintf(tw, "Rata-rata Posisi:\t%.2f\n", data.Summary.AveragePosition)
		}
		tw.Flush()
		fmt.Fprintln(w)
	}

	// 2. Diff (Perubahan dibanding run sebelumnya)
	if data.Diff != nil && data.Diff.HasPrevRun {
		fmt.Fprintf(w, "--- PERUBAHAN STATUS & PERINGKAT (DIFF) ---\n")
		fmt.Fprintf(tw, "Delta Clicks:\t%+.0f\n", data.Diff.DeltaClicks)
		fmt.Fprintf(tw, "Delta Impressions:\t%+.0f\n", data.Diff.DeltaImpressions)
		if data.Diff.DeltaAvgPosition != 0 {
			fmt.Fprintf(tw, "Delta Avg Position:\t%+.2f (negatif = membaik)\n", data.Diff.DeltaAvgPosition)
		}
		tw.Flush()

		if len(data.Diff.NewIndexed) > 0 {
			fmt.Fprintf(w, "\n[+] Baru Terindeks (%d):\n", len(data.Diff.NewIndexed))
			for _, u := range data.Diff.NewIndexed {
				fmt.Fprintf(w, "  [+] %s\n", u)
			}
		}
		if len(data.Diff.DeIndexed) > 0 {
			fmt.Fprintf(w, "\n[-] Hilang dari Indeks (%d):\n", len(data.Diff.DeIndexed))
			for _, u := range data.Diff.DeIndexed {
				fmt.Fprintf(w, "  [-] %s\n", u)
			}
		}
		if len(data.Diff.RankGains) > 0 {
			fmt.Fprintf(w, "\n[UP] Kenaikan Peringkat Signifikan (%d):\n", len(data.Diff.RankGains))
			for _, g := range data.Diff.RankGains {
				fmt.Fprintf(w, "  [UP] %s (%.1f -> %.1f, naik %+.1f)\n", g.URL, g.OldPosition, g.NewPosition, -g.Shift)
			}
		}
		if len(data.Diff.RankDrops) > 0 {
			fmt.Fprintf(w, "\n[DOWN] Penurunan Peringkat Signifikan (%d):\n", len(data.Diff.RankDrops))
			for _, d := range data.Diff.RankDrops {
				fmt.Fprintf(w, "  [DOWN] %s (%.1f -> %.1f, turun -%.1f)\n", d.URL, d.OldPosition, d.NewPosition, d.Shift)
			}
		}
		fmt.Fprintln(w)
	}

	// 3. Peluang Keyword Gap Teratas
	if len(data.TopGaps) > 0 {
		fmt.Fprintf(w, "--- PELUANG KEYWORD TERATAS (KEYWORD GAPS) ---\n")
		fmt.Fprintf(tw, "Tipe\tKata Kunci\tPosisi\tImp\tCTR\tMatch\tSkor\n")
		for _, g := range data.TopGaps {
			badge := fmt.Sprintf("[%s]", g.Category)
			fmt.Fprintf(tw, "%s\t%s\t%.1f\t%.0f\t%.1f%%\t%s\t%.1f\n",
				badge,
				g.Query,
				g.Position,
				g.Impressions,
				g.CTR*100,
				g.Match,
				g.Score,
			)
		}
		tw.Flush()
		fmt.Fprintln(w)
	}

	// 4. Masalah Audit / Indeksasi
	if len(data.AuditIssues) > 0 {
		fmt.Fprintf(w, "--- MASALAH AUDIT & KESEHATAN SEO ---\n")
		for _, issue := range data.AuditIssues {
			badge := "[WARN]"
			sev := strings.ToLower(issue.Severity)
			if sev == "error" {
				badge = "[ERROR]"
			} else if sev == "info" {
				badge = "[INFO]"
			}
			fmt.Fprintf(w, "  %s [%s] %s\n", badge, issue.Code, issue.Detail)
			if issue.URL != "" {
				fmt.Fprintf(w, "         URL: %s\n", issue.URL)
			}
		}
		fmt.Fprintln(w)
	}

	// 5. Daftar Halaman Teratas
	if len(data.Pages) > 0 {
		fmt.Fprintf(w, "--- DAFTAR HALAMAN (SAMPEL %d) ---\n", min(15, len(data.Pages)))
		fmt.Fprintf(tw, "Status\tURL\tClicks\tImp\tPos\n")

		sortedPages := make([]*model.Page, len(data.Pages))
		copy(sortedPages, data.Pages)
		sort.Slice(sortedPages, func(i, j int) bool {
			var c1, c2 float64
			if sortedPages[i].Perf != nil {
				c1 = sortedPages[i].Perf.Clicks
			}
			if sortedPages[j].Perf != nil {
				c2 = sortedPages[j].Perf.Clicks
			}
			return c1 > c2
		})

		limit := min(15, len(sortedPages))
		for i := 0; i < limit; i++ {
			p := sortedPages[i]
			status := "[UNINSPECTED]"
			if p.Index != nil {
				switch p.Index.Status {
				case model.StatusIndexed:
					status = "[OK]"
				case model.StatusCrawledNotIndexed, model.StatusDiscoveredNotIndexed, model.StatusUnknownToGoogle:
					status = "[WARN]"
				case model.StatusBlocked, model.StatusError, model.StatusDuplicate:
					status = "[ERROR]"
				default:
					status = "[UNKNOWN]"
				}
			}
			var clicks, imp, pos float64
			if p.Perf != nil {
				clicks = p.Perf.Clicks
				imp = p.Perf.Impressions
				pos = p.Perf.Position
			}
			fmt.Fprintf(tw, "%s\t%s\t%.0f\t%.0f\t%.1f\n", status, p.URL, clicks, imp, pos)
		}
		tw.Flush()
		fmt.Fprintln(w)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
