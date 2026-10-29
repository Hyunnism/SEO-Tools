package inspect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Clock adalah fungsi penyedia waktu untuk memudahkan mocking di test.
type Clock func() time.Time

// QuotaData merepresentasikan struktur data file quota.json.
type QuotaData struct {
	Date string `json:"date"` // YYYY-MM-DD dalam Pacific Time
	Used int    `json:"used"`
}

type QuotaManager struct {
	dataDir string
	clock   Clock
	mu      sync.Mutex
	loc     *time.Location
}

// NewQuotaManager membuat pengelola kuota harian inspeksi GSC.
// ponytail: kuota dilacak lokal per hari Pacific Time; fallback ke HTTP 429 bila ada pemakaian kuota di luar tool ini.
func NewQuotaManager(dataDir string, clock Clock) *QuotaManager {
	if clock == nil {
		clock = time.Now
	}

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fallback ke PST (UTC-8) jika timezone database tidak tersedia di OS
		loc = time.FixedZone("PST", -8*3600)
	}

	return &QuotaManager{
		dataDir: dataDir,
		clock:   clock,
		loc:     loc,
	}
}

// TodayPT mengembalikan tanggal hari ini dalam format YYYY-MM-DD zona waktu Pacific.
func (qm *QuotaManager) TodayPT() string {
	return qm.clock().In(qm.loc).Format("2006-01-02")
}

func (qm *QuotaManager) quotaFilePath(siteName string) string {
	return filepath.Join(qm.dataDir, siteName, "quota.json")
}

// GetUsedToday mengembalikan kuota yang sudah terpakai pada hari ini (Pacific Time).
// Jika tanggal file bukan hari ini, fungsi mengembalikan 0 (karena kuota sudah reset).
func (qm *QuotaManager) GetUsedToday(siteName string) (int, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	return qm.getUsedTodayLocked(siteName)
}

func (qm *QuotaManager) getUsedTodayLocked(siteName string) (int, error) {
	path := qm.quotaFilePath(siteName)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var q QuotaData
	if err := json.Unmarshal(b, &q); err != nil {
		return 0, fmt.Errorf("gagal unmarshal quota.json: %w", err)
	}

	today := qm.TodayPT()
	if q.Date != today {
		// Sudah berganti hari di Pacific Time -> reset ke 0
		return 0, nil
	}

	return q.Used, nil
}

// Increment menambah jumlah pemakaian kuota dan menyimpannya secara atomik ke disk.
func (qm *QuotaManager) Increment(siteName string, count int) error {
	if count <= 0 {
		return nil
	}

	qm.mu.Lock()
	defer qm.mu.Unlock()

	used, _ := qm.getUsedTodayLocked(siteName)
	newUsed := used + count
	today := qm.TodayPT()

	q := QuotaData{
		Date: today,
		Used: newUsed,
	}

	dir := filepath.Join(qm.dataDir, siteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	targetPath := qm.quotaFilePath(siteName)
	tmpPath := targetPath + ".tmp"

	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, targetPath)
}

// RemainingBudget menghitung sisa budget yang masih bisa dipakai hari ini.
func (qm *QuotaManager) RemainingBudget(siteName string, dailyBudget int) int {
	used, err := qm.GetUsedToday(siteName)
	if err != nil {
		return 0
	}
	remaining := dailyBudget - used
	if remaining < 0 {
		return 0
	}
	return remaining
}
