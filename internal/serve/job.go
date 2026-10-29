package serve

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrSiteAlreadyRunning = errors.New("run sedang berlangsung untuk situs ini")

// JobManager mengelola status job eksekusi pipeline secara thread-safe di memori.
// ponytail: in-memory tracker cukup untuk single-instance lokal; upgrade ke SQLite/disk jika perlu survive daemon restart.
type JobManager struct {
	mu           sync.RWMutex
	jobs         map[string]*JobState
	activeBySite map[string]string
}

// NewJobManager membuat instance baru JobManager.
func NewJobManager() *JobManager {
	return &JobManager{
		jobs:         make(map[string]*JobState),
		activeBySite: make(map[string]string),
	}
}

// CreateJob membuat job baru untuk situs tertentu bila belum ada yang aktif.
func (jm *JobManager) CreateJob(site string) (*JobState, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if activeID, exists := jm.activeBySite[site]; exists {
		if activeJob, ok := jm.jobs[activeID]; ok && (activeJob.Status == "queued" || activeJob.Status == "running") {
			return nil, ErrSiteAlreadyRunning
		}
	}

	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	id := fmt.Sprintf("run-%s-%d-%s", site, time.Now().Unix(), hex.EncodeToString(randBytes))

	job := &JobState{
		ID:        id,
		Site:      site,
		Status:    "queued",
		Step:      "init",
		StartedAt: time.Now().UTC(),
		Message:   "Menunggu antrian eksekusi...",
	}

	jm.jobs[id] = job
	jm.activeBySite[site] = id
	copied := *job
	return &copied, nil
}

// GetJob mengambil salinan status job berdasarkan ID.
func (jm *JobManager) GetJob(id string) (*JobState, bool) {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	job, ok := jm.jobs[id]
	if !ok {
		return nil, false
	}
	// Buat salinan dangkal untuk menghindari data race saat dibaca pemanggil
	copied := *job
	return &copied, true
}

// UpdateProgress memperbarui tahapan dan progres kuantitatif suatu job.
func (jm *JobManager) UpdateProgress(id string, step string, current, total int, msg string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	job, ok := jm.jobs[id]
	if !ok {
		return
	}
	job.Status = "running"
	if step != "" {
		job.Step = step
	}
	job.Current = current
	job.Total = total
	job.Message = msg
}

// CompleteJob menandai job selesai (sukses atau gagal) dan melepas kunci aktif situs.
func (jm *JobManager) CompleteJob(id string, err error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	job, ok := jm.jobs[id]
	if !ok {
		return
	}

	now := time.Now().UTC()
	job.EndedAt = &now
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		job.Message = fmt.Sprintf("Gagal: %v", err)
	} else {
		job.Status = "completed"
		job.Step = "done"
		if job.Total > 0 {
			job.Current = job.Total
		}
		job.Message = "Pipeline selesai dengan sukses."
	}

	delete(jm.activeBySite, job.Site)
}

// ActiveJobForSite memeriksa apakah ada job aktif untuk suatu situs.
func (jm *JobManager) ActiveJobForSite(site string) (*JobState, bool) {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	activeID, exists := jm.activeBySite[site]
	if !exists {
		return nil, false
	}
	job, ok := jm.jobs[activeID]
	if !ok || (job.Status != "queued" && job.Status != "running") {
		return nil, false
	}
	copied := *job
	return &copied, true
}
