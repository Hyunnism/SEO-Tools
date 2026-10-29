package indexing

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/model"
)

// ComputeContentHash menghitung hash SHA-256 dari konten on-page suatu halaman untuk mendeteksi perubahan.
func ComputeContentHash(p *model.Page) string {
	if p == nil {
		return ""
	}
	var sb strings.Builder
	if p.Crawl != nil {
		sb.WriteString(p.Crawl.Title)
		sb.WriteString("|")
		sb.WriteString(p.Crawl.MetaDesc)
		sb.WriteString("|")
		sb.WriteString(strings.Join(p.Crawl.H1, " "))
		sb.WriteString("|")
		sb.WriteString(strings.Join(p.Crawl.H2H3, " "))
		sb.WriteString("|")
		sb.WriteString(strings.Join(p.Crawl.Zones.Body, " "))
	} else {
		sb.WriteString(p.URL)
	}

	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:16]) // 32 hex chars
}

// BuildQueue menyusun antrean pengiriman URL yang terprioritas dan aman.
func BuildQueue(
	pages []*model.Page,
	store *SiteIndexingStore,
	cfg config.GoogleIndexingConfig,
	priorityPaths []string,
	aggressive bool,
	now time.Time,
) []PrioritizedIndexingItem {
	if store == nil {
		store = &SiteIndexingStore{Records: make(map[string]IndexingRecord)}
	}
	if store.Records == nil {
		store.Records = make(map[string]IndexingRecord)
	}

	cooldownDuration := time.Duration(cfg.ResubmitCooldownDays) * 24 * time.Hour
	maxSends := cfg.MaxSendsPerContentVersion
	if maxSends <= 0 {
		maxSends = 3
	}

	var candidates []PrioritizedIndexingItem

	for _, p := range pages {
		if p == nil || p.URL == "" {
			continue
		}

		currHash := ComputeContentHash(p)
		record, recorded := store.Records[p.URL]

		// 1. Cek Cooldown pengiriman per URL
		if recorded && now.Sub(record.SentAt) < cooldownDuration {
			continue
		}

		// 2. Cek Maksimal 3 Kiriman untuk Versi Konten yang Sama
		if recorded && record.ContentHash == currHash && record.SendCount >= maxSends {
			continue
		}

		// 3. Filter URL Dihapus (HTTP 404/410 tetapi sebelumnya indexed)
		isDeleted := false
		if p.Crawl != nil && (p.Crawl.HTTPStatus == http.StatusNotFound || p.Crawl.HTTPStatus == http.StatusGone) {
			if recorded || (p.Index != nil && p.Index.Status == model.StatusIndexed) {
				isDeleted = true
			}
		}

		if isDeleted {
			if aggressive && cfg.SendDeleted {
				candidates = append(candidates, PrioritizedIndexingItem{
					URL:         p.URL,
					Type:        TypeURLDeleted,
					Priority:    6,
					Reason:      "halaman 404/410 yang sebelumnya terindeks (URL_DELETED)",
					ContentHash: currHash,
				})
			}
			continue
		}

		// 4. Filter Keamanan: Hanya kirim URL berstatus HTTP 200, tanpa noindex, dan canonical ke diri sendiri
		if p.Crawl != nil {
			if p.Crawl.HTTPStatus != 0 && p.Crawl.HTTPStatus != http.StatusOK {
				continue
			}
			if strings.Contains(strings.ToLower(p.Crawl.RobotsMeta), "noindex") {
				continue
			}
			if p.Crawl.Canonical != "" && p.Crawl.Canonical != p.URL {
				continue
			}
		}
		if p.Index != nil && p.Index.Status == model.StatusBlocked {
			continue
		}

		// 5. Penentuan Prioritas
		isNew := !recorded && (p.Index == nil || p.Index.Status == model.StatusNeverInspected || p.Index.Status == model.StatusUnknownToGoogle)
		isContentChanged := recorded && record.ContentHash != "" && record.ContentHash != currHash
		isCrawledNotIndexed := p.Index != nil && p.Index.Status == model.StatusCrawledNotIndexed

		if isNew {
			// Prioritas 1: URL Baru
			candidates = append(candidates, PrioritizedIndexingItem{
				URL:         p.URL,
				Type:        TypeURLUpdated,
				Priority:    1,
				Reason:      "URL baru belum pernah dikirim",
				ContentHash: currHash,
			})
			continue
		}

		if isContentChanged {
			if aggressive && isCrawledNotIndexed {
				// Prioritas 4: Crawled Not Indexed dengan revisi konten
				candidates = append(candidates, PrioritizedIndexingItem{
					URL:         p.URL,
					Type:        TypeURLUpdated,
					Priority:    4,
					Reason:      "crawled - currently not indexed dengan revisi konten",
					ContentHash: currHash,
				})
			} else {
				// Prioritas 2: Konten Berubah sejak pengiriman terakhir
				candidates = append(candidates, PrioritizedIndexingItem{
					URL:         p.URL,
					Type:        TypeURLUpdated,
					Priority:    2,
					Reason:      "konten halaman berubah sejak pengiriman terakhir",
					ContentHash: currHash,
				})
			}
			continue
		}

		// Pada Safe Mode: HANYA URL baru atau konten berubah yang dikirim (berhenti di sini)
		if !aggressive {
			continue
		}

		// Prioritas Tambahan untuk Mode Agresif (P3 - P5)
		if p.Index != nil {
			switch p.Index.Status {
			case model.StatusDiscoveredNotIndexed:
				// Prioritas 3: Discovered Not Indexed (Google tahu tapi belum crawl)
				candidates = append(candidates, PrioritizedIndexingItem{
					URL:         p.URL,
					Type:        TypeURLUpdated,
					Priority:    3,
					Reason:      "discovered - currently not indexed (belum dicrawl Google)",
					ContentHash: currHash,
				})
				continue
			case model.StatusCrawledNotIndexed:
				// Jika konten sama, JANGAN dikirim lagi untuk menghindari pemborosan kuota
				continue
			}
		}

		// Prioritas 5: Halaman Prioritas (priority_paths) dengan usia crawl terlama
		for _, pref := range priorityPaths {
			if strings.HasPrefix(p.Path, pref) {
				candidates = append(candidates, PrioritizedIndexingItem{
					URL:         p.URL,
					Type:        TypeURLUpdated,
					Priority:    5,
					Reason:      "halaman jalur prioritas konfigurasi",
					ContentHash: currHash,
				})
				break
			}
		}
	}

	// Urutkan kandidat berdasarkan Prioritas (P1 -> P2 -> P3 -> P4 -> P5 -> P6)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].URL < candidates[j].URL
	})

	// Potong sesuai batasan daily_cap (Hard-Clamp 200)
	capLimit := cfg.DailyCap
	if capLimit <= 0 {
		capLimit = 50
	}
	if capLimit > 200 {
		capLimit = 200 // Hard-clamp kuota Google harian
	}

	if len(candidates) > capLimit {
		candidates = candidates[:capLimit]
	}

	return candidates
}
