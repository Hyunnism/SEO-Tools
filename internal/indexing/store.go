package indexing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"seo-boost/internal/model"
)

// LoadIndexingStore membaca riwayat pengiriman Indexing API dari data/<site>/indexing.json.
func LoadIndexingStore(dataDir, site string) (*SiteIndexingStore, error) {
	filePath := filepath.Join(dataDir, site, "indexing.json")
	b, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &SiteIndexingStore{
				Schema:    1,
				Site:      site,
				UpdatedAt: time.Now().UTC(),
				Records:   make(map[string]IndexingRecord),
			}, nil
		}
		return nil, fmt.Errorf("gagal membaca indexing.json untuk %s: %w", site, err)
	}

	var s SiteIndexingStore
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("gagal unmarshal indexing.json: %w", err)
	}

	if s.Records == nil {
		s.Records = make(map[string]IndexingRecord)
	}
	return &s, nil
}

// SaveIndexingStore menyimpan riwayat pengiriman ke data/<site>/indexing.json secara atomik.
func SaveIndexingStore(dataDir, site string, s *SiteIndexingStore) error {
	dir := filepath.Join(dataDir, site)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("gagal membuat direktori %s: %w", dir, err)
	}

	s.Schema = 1
	s.Site = site
	s.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("gagal marshal indexing.json: %w", err)
	}

	targetPath := filepath.Join(dir, "indexing.json")
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("gagal menulis berkas sementara: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("gagal rename berkas sementara ke %s: %w", targetPath, err)
	}

	return nil
}

// CalculateAverageCrawlLatency mengukur rata-rata selisih waktu antara pengiriman ke Indexing API
// dan waktu crawl pertama Google yang lebih baru dari waktu kirim.
func CalculateAverageCrawlLatency(pages []*model.Page, store *SiteIndexingStore) (time.Duration, int) {
	if store == nil || len(store.Records) == 0 {
		return 0, 0
	}

	var totalLatency time.Duration
	var count int

	for _, p := range pages {
		if p == nil || p.Index == nil || p.Index.LastCrawlTime == nil {
			continue
		}
		rec, exists := store.Records[p.URL]
		if !exists || rec.SentAt.IsZero() {
			continue
		}

		crawlTime := *p.Index.LastCrawlTime
		if crawlTime.After(rec.SentAt) {
			latency := crawlTime.Sub(rec.SentAt)
			totalLatency += latency
			count++
		}
	}

	if count == 0 {
		return 0, 0
	}
	return totalLatency / time.Duration(count), count
}
