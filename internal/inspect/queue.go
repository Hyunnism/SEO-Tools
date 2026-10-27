package inspect

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"seo-boost/internal/google"
	"seo-boost/internal/model"
)

type PrioritizedURL struct {
	URL      string
	Priority int // 1 (tertinggi) s.d. 5 (terendah)
	Reason   string
	Page     *model.Page
}

// BuildInspectQueue menyusun dan memprioritaskan daftar URL yang perlu diinspeksi.
func BuildInspectQueue(pages []*model.Page, priorityPaths []string, reinspectAfterDays int, force bool, now time.Time) []PrioritizedURL {
	if reinspectAfterDays <= 0 {
		reinspectAfterDays = 30
	}
	reinspectCutoff := now.AddDate(0, 0, -reinspectAfterDays)

	var candidates []PrioritizedURL

	for _, p := range pages {
		u := p.URL
		path := p.Path
		if path == "" {
			if parsed, err := url.Parse(u); err == nil {
				path = parsed.Path
			}
		}

		// 1. Belum pernah diinspeksi
		if p.Index == nil || p.Index.InspectedAt.IsZero() || p.Index.Status == model.StatusNeverInspected {
			candidates = append(candidates, PrioritizedURL{
				URL:      u,
				Priority: 1,
				Reason:   "belum pernah diinspeksi",
				Page:     p,
			})
			continue
		}

		// 2. Cocok dengan priority_paths
		if isPriorityPath(path, priorityPaths) {
			// Jika belum lewat masa reinspect dan tidak di-force, bisa dilewati jika indexed
			if !force && p.Index.Status == model.StatusIndexed && p.Index.InspectedAt.After(reinspectCutoff) {
				continue
			}
			candidates = append(candidates, PrioritizedURL{
				URL:      u,
				Priority: 2,
				Reason:   "priority path",
				Page:     p,
			})
			continue
		}

		// 3. Status bermasalah pada inspeksi sebelumnya
		if isTroubledStatus(p.Index.Status) {
			candidates = append(candidates, PrioritizedURL{
				URL:      u,
				Priority: 3,
				Reason:   "status sebelumnya bermasalah (" + string(p.Index.Status) + ")",
				Page:     p,
			})
			continue
		}

		// 4. Status indexed tapi usia inspeksinya sudah lewat reinspectAfterDays (atau force)
		if p.Index.Status == model.StatusIndexed {
			if force || p.Index.InspectedAt.Before(reinspectCutoff) {
				candidates = append(candidates, PrioritizedURL{
					URL:      u,
					Priority: 4,
					Reason:   "usia inspeksi > batasan hari",
					Page:     p,
				})
			}
			continue
		}

		// 5. Sisanya (misal duplicate, other, blocked)
		candidates = append(candidates, PrioritizedURL{
			URL:      u,
			Priority: 5,
			Reason:   "inspeksi berkala",
			Page:     p,
		})
	}

	// Urutkan kandidat: priority naik (1 -> 5), lalu yang paling lama diinspeksi
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		var t1, t2 time.Time
		if candidates[i].Page.Index != nil {
			t1 = candidates[i].Page.Index.InspectedAt
		}
		if candidates[j].Page.Index != nil {
			t2 = candidates[j].Page.Index.InspectedAt
		}
		return t1.Before(t2)
	})

	return candidates
}

func isPriorityPath(path string, priorityPaths []string) bool {
	for _, pp := range priorityPaths {
		if strings.HasPrefix(path, pp) {
			return true
		}
	}
	return false
}

func isTroubledStatus(st model.IndexStatus) bool {
	switch st {
	case model.StatusCrawledNotIndexed,
		model.StatusDiscoveredNotIndexed,
		model.StatusUnknownToGoogle,
		model.StatusError:
		return true
	}
	return false
}

type InspectWorkerConfig struct {
	RPM      int // Request per minute (misal 600)
	SiteName string
	Property string
}

// InspectBatch menjalankan URL Inspection untuk daftar item dalam antrian
// dengan mematuhi rate limit dan otomatis berhenti jika kuota habis (HTTP 429).
func InspectBatch(
	ctx context.Context,
	client *google.Client,
	qm *QuotaManager,
	items []PrioritizedURL,
	cfg InspectWorkerConfig,
	onItemDone func(item PrioritizedURL, info *model.IndexInfo, err error),
) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	rpm := cfg.RPM
	if rpm <= 0 {
		rpm = 600
	}
	interval := time.Duration(float64(time.Minute) / float64(rpm))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	processedCount := 0

	for _, item := range items {
		select {
		case <-ctx.Done():
			return processedCount, ctx.Err()
		case <-ticker.C:
		}

		rawResult, err := client.InspectURL(ctx, cfg.Property, item.URL)
		if err != nil {
			if qm != nil {
				_ = qm.Increment(cfg.SiteName, 1)
			}
			processedCount++

			if onItemDone != nil {
				onItemDone(item, nil, err)
			}

			// Cek apakah kuota habis / 429
			if google.IsErrQuotaExceeded(err) {
				return processedCount, errors.New("kuota URL Inspection harian Google habis (HTTP 429); inspeksi dihentikan")
			}
			continue
		}

		info := MapIndexStatus(rawResult)
		item.Page.Index = info
		if qm != nil {
			_ = qm.Increment(cfg.SiteName, 1)
		}
		processedCount++

		if onItemDone != nil {
			onItemDone(item, info, nil)
		}
	}

	return processedCount, nil
}
