package serve

import (
	"context"
	"time"

	"seo-boost/internal/config"
)

// ProgressCallback dipanggil oleh runner untuk melaporkan kemajuan pipeline.
type ProgressCallback func(step string, current, total int, message string)

// Runner mengeksekusi pipeline SEO untuk suatu situs dengan callback progres.
type Runner func(ctx context.Context, site *config.Site, progress ProgressCallback) error

// JobState merepresentasikan status eksekusi pipeline asinkron.
type JobState struct {
	ID        string     `json:"id"`
	Site      string     `json:"site"`
	Status    string     `json:"status"` // "queued", "running", "completed", "failed"
	Step      string     `json:"step"`   // "discover", "crawl", "inspect", "perf", "gap", "store", "done"
	Current   int        `json:"current"`
	Total     int        `json:"total"`
	Message   string     `json:"message"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// SiteSummary merangkum statistik terkini untuk satu situs di dashboard.
type SiteSummary struct {
	Name         string     `json:"name"`
	BaseURL      string     `json:"base_url"`
	GSCProperty  string     `json:"gsc_property"`
	TotalPages   int        `json:"total_pages"`
	IndexedPages int        `json:"indexed_pages"`
	IssuesCount  int        `json:"issues_count"`
	LastRun      *time.Time `json:"last_run,omitempty"`
}
