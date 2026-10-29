package report

import (
	"sort"

	"seo-boost/internal/model"
)

// RankShift mencatat perubahan posisi halaman antar run.
type RankShift struct {
	URL         string  `json:"url"`
	OldPosition float64 `json:"old_position"`
	NewPosition float64 `json:"new_position"`
	Shift       float64 `json:"shift"` // NewPosition - OldPosition (positif = turun peringkat, negatif = naik)
}

// RunDiff mencatat delta antara run saat ini dengan run sebelumnya.
type RunDiff struct {
	HasPrevRun       bool        `json:"has_prev_run"`
	NewIndexed       []string    `json:"new_indexed,omitempty"`
	DeIndexed        []string    `json:"deindexed,omitempty"`
	RankDrops        []RankShift `json:"rank_drops,omitempty"` // shift >= 3.0 (posisi numerik naik = ranking turun)
	RankGains        []RankShift `json:"rank_gains,omitempty"` // shift <= -3.0 (posisi numerik turun = ranking naik)
	DeltaClicks      float64     `json:"delta_clicks"`
	DeltaImpressions float64     `json:"delta_impressions"`
	DeltaAvgPosition float64     `json:"delta_avg_position"`
}

// ComputeDiff menghitung perbedaan antara currentPages dan prevRun.
func ComputeDiff(currentPages []*model.Page, prevRun *model.RunSummary) *RunDiff {
	diff := &RunDiff{}
	if prevRun == nil || prevRun.PageSummaries == nil {
		return diff
	}
	diff.HasPrevRun = true

	var totalClicks, totalImp, posSum float64
	var posCount int

	currMap := make(map[string]*model.Page, len(currentPages))
	for _, p := range currentPages {
		currMap[p.URL] = p
		if p.Perf != nil {
			totalClicks += p.Perf.Clicks
			totalImp += p.Perf.Impressions
			if p.Perf.Position > 0 {
				posSum += p.Perf.Position
				posCount++
			}
		}

		oldStub, existed := prevRun.PageSummaries[p.URL]
		currIndexed := p.Index != nil && p.Index.Status == model.StatusIndexed

		if currIndexed {
			if !existed || oldStub.Status != model.StatusIndexed {
				diff.NewIndexed = append(diff.NewIndexed, p.URL)
			}
		}

		if existed && oldStub.Position > 0 && p.Perf != nil && p.Perf.Position > 0 {
			shift := p.Perf.Position - oldStub.Position
			// Jika shift >= 3.0: posisi memburuk (misal dari 3.0 ke 6.0 = drop)
			if shift >= 3.0 {
				diff.RankDrops = append(diff.RankDrops, RankShift{
					URL:         p.URL,
					OldPosition: oldStub.Position,
					NewPosition: p.Perf.Position,
					Shift:       shift,
				})
			} else if shift <= -3.0 {
				// Naik peringkat (misal dari 8.0 ke 4.5 = gain)
				diff.RankGains = append(diff.RankGains, RankShift{
					URL:         p.URL,
					OldPosition: oldStub.Position,
					NewPosition: p.Perf.Position,
					Shift:       shift,
				})
			}
		}
	}

	// Cek DeIndexed: sebelumnya indexed, sekarang tidak ada atau status != indexed
	for url, stub := range prevRun.PageSummaries {
		if stub.Status == model.StatusIndexed {
			curr, exists := currMap[url]
			if !exists || curr.Index == nil || curr.Index.Status != model.StatusIndexed {
				diff.DeIndexed = append(diff.DeIndexed, url)
			}
		}
	}

	var currAvgPos float64
	if posCount > 0 {
		currAvgPos = posSum / float64(posCount)
	}

	diff.DeltaClicks = totalClicks - prevRun.TotalClicks
	diff.DeltaImpressions = totalImp - prevRun.TotalImpressions
	diff.DeltaAvgPosition = currAvgPos - prevRun.AveragePosition

	sort.Strings(diff.NewIndexed)
	sort.Strings(diff.DeIndexed)
	sort.Slice(diff.RankDrops, func(i, j int) bool {
		return diff.RankDrops[i].Shift > diff.RankDrops[j].Shift
	})
	sort.Slice(diff.RankGains, func(i, j int) bool {
		return diff.RankGains[i].Shift < diff.RankGains[j].Shift
	})

	return diff
}
