package crawl

import (
	"fmt"
	"strings"

	"seo-boost/internal/model"
)

// Konstanta ambang batas audit on-page SEO (tersentralisasi).
const (
	MinTitleLen = 30
	MaxTitleLen = 60
	MinMetaLen  = 70
	MaxMetaLen  = 160
	MinWordLen  = 300 // blog/artikel
)

// AuditPage mengevaluasi aturan audit SEO on-page pada sebuah halaman.
func AuditPage(p *model.Page) []model.AuditIssue {
	var issues []model.AuditIssue
	c := p.Crawl
	if c == nil {
		return issues
	}

	// 1. HTTP Status
	if c.HTTPStatus != 200 {
		issues = append(issues, model.AuditIssue{
			Code:     "http_not_200",
			Severity: "error",
			Detail:   fmt.Sprintf("Status HTTP adalah %d (bukan 200 OK)", c.HTTPStatus),
		})
		// Jika halaman tidak 200, audit on-page konten lainnya tidak relevan
		return issues
	}

	// 2. Robots Meta (noindex)
	if strings.Contains(strings.ToLower(c.RobotsMeta), "noindex") {
		issues = append(issues, model.AuditIssue{
			Code:     "noindex_meta",
			Severity: "error",
			Detail:   fmt.Sprintf("Tag meta robots memuat 'noindex': %q", c.RobotsMeta),
		})
	}

	// 3. Title
	if strings.TrimSpace(c.Title) == "" {
		issues = append(issues, model.AuditIssue{
			Code:     "title_missing",
			Severity: "error",
			Detail:   "Elemen <title> tidak ditemukan",
		})
	} else {
		titleLen := len([]rune(strings.TrimSpace(c.Title)))
		if titleLen < MinTitleLen || titleLen > MaxTitleLen {
			issues = append(issues, model.AuditIssue{
				Code:     "title_length",
				Severity: "warn",
				Detail:   fmt.Sprintf("Panjang judul %d karakter (rekomendasi: %d-%d)", titleLen, MinTitleLen, MaxTitleLen),
			})
		}
	}

	// 4. Meta Description
	if strings.TrimSpace(c.MetaDesc) == "" {
		issues = append(issues, model.AuditIssue{
			Code:     "meta_missing",
			Severity: "warn",
			Detail:   "Tag meta description tidak ditemukan",
		})
	} else {
		metaLen := len([]rune(strings.TrimSpace(c.MetaDesc)))
		if metaLen < MinMetaLen || metaLen > MaxMetaLen {
			issues = append(issues, model.AuditIssue{
				Code:     "meta_length",
				Severity: "warn",
				Detail:   fmt.Sprintf("Panjang meta description %d karakter (rekomendasi: %d-%d)", metaLen, MinMetaLen, MaxMetaLen),
			})
		}
	}

	// 5. H1
	if len(c.H1) == 0 {
		issues = append(issues, model.AuditIssue{
			Code:     "h1_missing",
			Severity: "error",
			Detail:   "Tag <h1> tidak ditemukan pada halaman",
		})
	} else if len(c.H1) > 1 {
		issues = append(issues, model.AuditIssue{
			Code:     "h1_multiple",
			Severity: "warn",
			Detail:   fmt.Sprintf("Ditemukan %d tag <h1> (disarankan hanya 1 per halaman)", len(c.H1)),
		})
	}

	// 6. Canonical
	if strings.TrimSpace(c.Canonical) == "" {
		issues = append(issues, model.AuditIssue{
			Code:     "canonical_missing",
			Severity: "warn",
			Detail:   "Tag link rel=canonical tidak ditemukan",
		})
	} else {
		// Normalisasi sederhana untuk cek mismatch
		cTrim := strings.TrimRight(strings.ToLower(c.Canonical), "/")
		uTrim := strings.TrimRight(strings.ToLower(p.URL), "/")
		if cTrim != uTrim {
			issues = append(issues, model.AuditIssue{
				Code:     "canonical_mismatch",
				Severity: "warn",
				Detail:   fmt.Sprintf("Canonical mengarah ke URL lain (%s)", c.Canonical),
			})
		}
	}

	// 7. Images without alt
	if c.ImagesNoAlt > 0 {
		issues = append(issues, model.AuditIssue{
			Code:     "images_no_alt",
			Severity: "warn",
			Detail:   fmt.Sprintf("Ditemukan %d gambar tanpa atribut alt", c.ImagesNoAlt),
		})
	}

	// 8. Thin Content & SPA
	if c.ThinHTML {
		issues = append(issues, model.AuditIssue{
			Code:     "thin_html_spa",
			Severity: "info",
			Detail:   "Diduga halaman SPA / dirender JavaScript; teks yang terlihat crawler tipis",
		})
	} else if c.WordCount < MinWordLen {
		issues = append(issues, model.AuditIssue{
			Code:     "thin_content",
			Severity: "warn",
			Detail:   fmt.Sprintf("Jumlah kata hanya %d (rekomendasi: >= %d kata)", c.WordCount, MinWordLen),
		})
	}

	// 9. JSON-LD
	if !c.HasJSONLD {
		issues = append(issues, model.AuditIssue{
			Code:     "jsonld_missing",
			Severity: "info",
			Detail:   "Tidak ditemukan structured data Schema JSON-LD",
		})
	}

	// 10. Orphan Page
	if p.InSitemap && p.InternalIn == 0 {
		issues = append(issues, model.AuditIssue{
			Code:     "orphan_page",
			Severity: "warn",
			Detail:   "Halaman ada di sitemap tetapi tidak memiliki tautan internal masuk (orphan page)",
		})
	}

	return issues
}

// AuditSiteWide memeriksa isu lintas halaman (misalnya duplicate title dan duplicate meta description).
func AuditSiteWide(pages []*model.Page) {
	titleMap := make(map[string][]*model.Page)
	metaMap := make(map[string][]*model.Page)

	for _, p := range pages {
		if p.Crawl == nil || p.Crawl.HTTPStatus != 200 {
			continue
		}
		t := strings.TrimSpace(p.Crawl.Title)
		if t != "" {
			titleMap[t] = append(titleMap[t], p)
		}
		m := strings.TrimSpace(p.Crawl.MetaDesc)
		if m != "" {
			metaMap[m] = append(metaMap[m], p)
		}
	}

	for t, group := range titleMap {
		if len(group) > 1 {
			for _, p := range group {
				p.Audit = append(p.Audit, model.AuditIssue{
					Code:     "title_duplicate",
					Severity: "warn",
					Detail:   fmt.Sprintf("Judul sama dengan %d halaman lain: %q", len(group)-1, t),
				})
			}
		}
	}

	for _, group := range metaMap {
		if len(group) > 1 {
			for _, p := range group {
				p.Audit = append(p.Audit, model.AuditIssue{
					Code:     "meta_duplicate",
					Severity: "warn",
					Detail:   fmt.Sprintf("Meta description sama dengan %d halaman lain", len(group)-1),
				})
			}
		}
	}
}
