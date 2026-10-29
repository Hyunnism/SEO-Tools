package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"seo-boost/internal/model"
)

const (
	CurrentSchema = 1
	MaxRunHistory = 90
)

// PagesEnvelope membungkus snapshot []Page dengan metadata skema.
type PagesEnvelope struct {
	Schema    int           `json:"schema"`
	Site      string        `json:"site"`
	UpdatedAt time.Time     `json:"updated_at"`
	Pages     []*model.Page `json:"pages"`
}

type Store struct {
	dataDir string
	mu      sync.RWMutex
}

func NewStore(dataDir string) *Store {
	if dataDir == "" {
		dataDir = "./data"
	}
	return &Store{
		dataDir: dataDir,
	}
}

// Lock mengunci akses eksklusif ke direktori situs.
func (s *Store) Lock(site string) (func(), error) {
	return Lock(s.dataDir, site)
}

func (s *Store) siteDir(site string) string {
	return filepath.Join(s.dataDir, site)
}

func (s *Store) pagesFilePath(site string) string {
	return filepath.Join(s.siteDir(site), "pages.json")
}

func (s *Store) runsDir(site string) string {
	return filepath.Join(s.siteDir(site), "runs")
}

// SavePages menyimpan snapshot halaman ke data/<site>/pages.json secara atomik.
func (s *Store) SavePages(site string, pages []*model.Page) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.siteDir(site)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	env := PagesEnvelope{
		Schema:    CurrentSchema,
		Site:      site,
		UpdatedAt: time.Now().UTC(),
		Pages:     pages,
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("gagal marshal pages.json: %w", err)
	}

	targetPath := s.pagesFilePath(site)
	return atomicWriteFile(targetPath, data)
}

// LoadPages membaca snapshot halaman dari data/<site>/pages.json.
func (s *Store) LoadPages(site string) ([]*model.Page, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.pagesFilePath(site)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var env PagesEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("gagal unmarshal pages.json: %w", err)
	}

	return env.Pages, nil
}

// SaveRun menyimpan ringkasan run ke data/<site>/runs/<timestamp>.json dan memangkas riwayat lama.
func (s *Store) SaveRun(site string, run *model.RunSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.runsDir(site)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if run.Timestamp.IsZero() {
		run.Timestamp = time.Now().UTC()
	}
	run.Schema = CurrentSchema
	run.Site = site

	// Format nama file: 2006-01-02T15-04-05Z.json (aman untuk filesystem Windows yang melarang colon ':')
	filename := strings.ReplaceAll(run.Timestamp.UTC().Format("2006-01-02T15-04-05Z"), ":", "-") + ".json"
	targetPath := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("gagal marshal run summary: %w", err)
	}

	if err := atomicWriteFile(targetPath, data); err != nil {
		return err
	}

	// Pangkas riwayat lama (pertahankan 90 file terbaru)
	_ = s.pruneRunsLocked(dir, MaxRunHistory)
	return nil
}

// LoadLatestRun mengambil run summary terbaru.
func (s *Store) LoadLatestRun(site string) (*model.RunSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.listRunFilesLocked(s.runsDir(site))
	if err != nil || len(files) == 0 {
		return nil, os.ErrNotExist
	}

	latestPath := filepath.Join(s.runsDir(site), files[len(files)-1])
	return loadRunFile(latestPath)
}

// LoadPreviousRun mengambil run summary satu run sebelum yang terbaru untuk keperluan diff.
func (s *Store) LoadPreviousRun(site string) (*model.RunSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.listRunFilesLocked(s.runsDir(site))
	if err != nil || len(files) < 2 {
		return nil, os.ErrNotExist
	}

	prevPath := filepath.Join(s.runsDir(site), files[len(files)-2])
	return loadRunFile(prevPath)
}

// ListRuns mengembalikan daftar nama berkas run yang tersimpan (terurut dari terlama ke terbaru).
func (s *Store) ListRuns(site string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.listRunFilesLocked(s.runsDir(site))
}

// LoadRunByID memuat run summary berdasarkan runID (nama berkas atau timestamp).
func (s *Store) LoadRunByID(site string, runID string) (*model.RunSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !strings.HasSuffix(runID, ".json") {
		runID += ".json"
	}
	targetPath := filepath.Join(s.runsDir(site), runID)
	return loadRunFile(targetPath)
}

func loadRunFile(path string) (*model.RunSummary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run model.RunSummary
	if err := json.Unmarshal(b, &run); err != nil {
		return nil, fmt.Errorf("gagal unmarshal run file %s: %w", path, err)
	}
	return &run, nil
}

func (s *Store) listRunFilesLocked(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".tmp") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func (s *Store) pruneRunsLocked(dir string, maxKeep int) error {
	files, err := s.listRunFilesLocked(dir)
	if err != nil {
		return err
	}

	if len(files) <= maxKeep {
		return nil
	}

	toRemove := len(files) - maxKeep
	for i := 0; i < toRemove; i++ {
		_ = os.Remove(filepath.Join(dir, files[i]))
	}
	return nil
}

// atomicWriteFile menulis konten ke file.tmp lalu rename secara atomik.
func atomicWriteFile(targetPath string, data []byte) error {
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

// BuildRunSummary membuat RunSummary dari daftar Page saat ini.
func BuildRunSummary(site string, pages []*model.Page) *model.RunSummary {
	run := &model.RunSummary{
		Schema:        CurrentSchema,
		Site:          site,
		Timestamp:     time.Now().UTC(),
		TotalPages:    len(pages),
		PageSummaries: make(map[string]model.PageStub, len(pages)),
	}

	var posSum float64
	var posCount int

	for _, p := range pages {
		stub := model.PageStub{}
		if p.Index != nil {
			stub.Status = p.Index.Status
			if p.Index.Status == model.StatusIndexed {
				run.IndexedCount++
			} else {
				run.TroubledCount++
			}
		}

		if p.Perf != nil {
			stub.Position = p.Perf.Position
			stub.Clicks = p.Perf.Clicks
			stub.Page1 = p.Perf.Page1

			run.TotalClicks += p.Perf.Clicks
			run.TotalImpressions += p.Perf.Impressions
			if p.Perf.Page1 {
				run.Page1Count++
			}
			if p.Perf.Position > 0 {
				posSum += p.Perf.Position
				posCount++
			}
		}

		run.PageSummaries[p.URL] = stub
	}

	if posCount > 0 {
		run.AveragePosition = posSum / float64(posCount)
	}

	return run
}

