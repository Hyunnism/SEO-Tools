package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"seo-boost/internal/config"
	"seo-boost/internal/model"
	"seo-boost/web"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) findSite(name string) (*config.Site, error) {
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].Name == name {
			return &s.cfg.Sites[i], nil
		}
	}
	// Anti-SSRF: menolak akses ke situs yang tidak ada di config
	return nil, fmt.Errorf("site %q tidak ditemukan di konfigurasi", name)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := web.Assets.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Gagal memuat template antarmuka dashboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleAPISites(w http.ResponseWriter, r *http.Request) {
	var summaries []SiteSummary

	for _, site := range s.cfg.Sites {
		sum := SiteSummary{
			Name:        site.Name,
			BaseURL:     site.BaseURL,
			GSCProperty: site.GSCProperty,
		}

		if latestRun, err := s.st.LoadLatestRun(site.Name); err == nil && latestRun != nil {
			sum.TotalPages = latestRun.TotalPages
			sum.IndexedPages = latestRun.IndexedCount
			sum.IssuesCount = latestRun.TroubledCount
			t := latestRun.Timestamp
			sum.LastRun = &t
		} else if pages, err := s.st.LoadPages(site.Name); err == nil && len(pages) > 0 {
			sum.TotalPages = len(pages)
			for _, p := range pages {
				if p.Index != nil && p.Index.Status == model.StatusIndexed {
					sum.IndexedPages++
				}
				if (p.Crawl != nil && p.Crawl.HTTPStatus != 200) || len(p.Audit) > 0 || (p.Index != nil && p.Index.Status != model.StatusIndexed) {
					sum.IssuesCount++
				}
			}
		}

		summaries = append(summaries, sum)
	}

	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleAPISitePages(w http.ResponseWriter, r *http.Request) {
	siteName := r.PathValue("name")
	site, err := s.findSite(siteName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	pages, err := s.st.LoadPages(site.Name)
	if err != nil {
		// Jika belum ada pages.json, kembalikan array kosong bukan error 500
		writeJSON(w, http.StatusOK, []*model.Page{})
		return
	}

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	var filtered []*model.Page
	for _, p := range pages {
		if statusFilter != "" && statusFilter != "all" {
			if statusFilter == "error" {
				isErr := (p.Crawl != nil && p.Crawl.HTTPStatus != 200) || len(p.Audit) > 0 || (p.Index != nil && p.Index.Status != model.StatusIndexed)
				if !isErr {
					continue
				}
			} else {
				if p.Index == nil || string(p.Index.Status) != statusFilter {
					continue
				}
			}
		}

		if q != "" {
			matches := strings.Contains(strings.ToLower(p.URL), q) ||
				strings.Contains(strings.ToLower(p.Path), q) ||
				(p.Crawl != nil && strings.Contains(strings.ToLower(p.Crawl.Title), q))
			if !matches {
				continue
			}
		}

		filtered = append(filtered, p)
	}

	if filtered == nil {
		filtered = []*model.Page{}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleAPISiteGaps(w http.ResponseWriter, r *http.Request) {
	siteName := r.PathValue("name")
	site, err := s.findSite(siteName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	pages, err := s.st.LoadPages(site.Name)
	if err != nil {
		writeJSON(w, http.StatusOK, []model.QueryGap{})
		return
	}

	var allGaps []model.QueryGap
	for _, p := range pages {
		allGaps = append(allGaps, p.Gaps...)
	}

	sort.Slice(allGaps, func(i, j int) bool {
		return allGaps[i].Score > allGaps[j].Score
	})

	if allGaps == nil {
		allGaps = []model.QueryGap{}
	}
	writeJSON(w, http.StatusOK, allGaps)
}

func (s *Server) handleAPISiteRuns(w http.ResponseWriter, r *http.Request) {
	siteName := r.PathValue("name")
	site, err := s.findSite(siteName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	runs, err := s.st.ListRuns(site.Name)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	if runs == nil {
		runs = []string{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleAPISiteRunDetail(w http.ResponseWriter, r *http.Request) {
	siteName := r.PathValue("name")
	runID := r.PathValue("id")

	site, err := s.findSite(siteName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	run, err := s.st.LoadRunByID(site.Name, runID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("run %q tidak ditemukan", runID)})
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleAPISiteTriggerRun(w http.ResponseWriter, r *http.Request) {
	siteName := r.PathValue("name")
	site, err := s.findSite(siteName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if s.runner == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "runner tidak dikonfigurasi pada server"})
		return
	}

	job, err := s.jobs.CreateJob(site.Name)
	if errors.Is(err, ErrSiteAlreadyRunning) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	jobID := job.ID
	jobStatus := job.Status

	// Jalankan pipeline di goroutine latar belakang
	go func(id string, sCfg *config.Site) {
		ctx := context.Background()
		rErr := s.runner(ctx, sCfg, func(step string, current, total int, msg string) {
			s.jobs.UpdateProgress(id, step, current, total, msg)
		})
		s.jobs.CompleteJob(id, rErr)
	}(jobID, site)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"run_id": jobID,
		"status": jobStatus,
		"site":   site.Name,
	})
}

func (s *Server) handleAPIRunProgress(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	job, ok := s.jobs.GetJob(runID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job run tidak ditemukan"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}
