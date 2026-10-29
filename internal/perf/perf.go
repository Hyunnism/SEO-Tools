package perf

import (
	"context"
	"sort"
	"time"

	"seo-boost/internal/google"
	"seo-boost/internal/model"
	"seo-boost/internal/sitemap"
)

// CalculateDateRange menghitung rentang tanggal GSC final:
// endDate = today - 3 hari (latensi data final GSC)
// startDate = endDate - (perfDays - 1)
func CalculateDateRange(today time.Time, perfDays int) (string, string) {
	if perfDays <= 0 {
		perfDays = 28
	}
	endDateT := today.AddDate(0, 0, -3)
	startDateT := endDateT.AddDate(0, 0, -(perfDays - 1))

	return startDateT.Format("2006-01-02"), endDateT.Format("2006-01-02")
}

// CalculatePreviousDateRange menghitung rentang tanggal GSC periode pembanding sebelumnya:
// prevEndDate = today - 3 - perfDays
// prevStartDate = prevEndDate - (perfDays - 1)
func CalculatePreviousDateRange(today time.Time, perfDays int) (string, string) {
	if perfDays <= 0 {
		perfDays = 28
	}
	prevEndDateT := today.AddDate(0, 0, -(3 + perfDays))
	prevStartDateT := prevEndDateT.AddDate(0, 0, -(perfDays - 1))

	return prevStartDateT.Format("2006-01-02"), prevEndDateT.Format("2006-01-02")
}

// CalculateDeltas menghitung delta performa antara periode saat ini vs periode sebelumnya
// dan menyimpannya ke field Delta pada current map.
func CalculateDeltas(current, previous map[string]*model.PagePerf) {
	if current == nil {
		return
	}
	for url, curr := range current {
		if prev, ok := previous[url]; ok && prev != nil {
			posDelta := 0.0
			if prev.Position > 0 && curr.Position > 0 {
				posDelta = curr.Position - prev.Position
			}
			curr.Delta = &model.PerfDelta{
				Clicks:      curr.Clicks - prev.Clicks,
				Impressions: curr.Impressions - prev.Impressions,
				Position:    posDelta,
			}
		} else {
			curr.Delta = &model.PerfDelta{
				Clicks:      curr.Clicks,
				Impressions: curr.Impressions,
				Position:    0,
			}
		}
	}
}

type Aggregator struct {
	client  *google.Client
	baseURL string
}

func NewAggregator(client *google.Client, baseURL string) *Aggregator {
	return &Aggregator{
		client:  client,
		baseURL: baseURL,
	}
}

// FetchAndAggregate mengambil data Search Analytics dari GSC dan mengagregasikan
// metrik pencarian (clicks, impressions, weighted position, CTR, Page1) per halaman.
func (a *Aggregator) FetchAndAggregate(ctx context.Context, gscProperty string, startDate, endDate string) (map[string]*model.PagePerf, error) {
	rows, err := a.client.QuerySearchAnalytics(ctx, gscProperty, startDate, endDate, 0)
	if err != nil {
		return nil, err
	}

	return a.AggregateRows(rows), nil
}

type pageAccumulator struct {
	clicks         float64
	impressions    float64
	weightedPosSum float64
	queries        []model.QueryPerf
}

// AggregateRows mengelompokkan baris data GSC (page x query) ke PagePerf per URL.
func (a *Aggregator) AggregateRows(rows []google.SearchAnalyticsRow) map[string]*model.PagePerf {
	accMap := make(map[string]*pageAccumulator)

	for _, row := range rows {
		if len(row.Keys) < 2 {
			continue
		}
		rawPageURL := row.Keys[0]
		query := row.Keys[1]

		normURL, err := sitemap.NormalizeURL(rawPageURL, a.baseURL)
		if err != nil {
			normURL = rawPageURL
		}

		acc, exists := accMap[normURL]
		if !exists {
			acc = &pageAccumulator{}
			accMap[normURL] = acc
		}

		acc.clicks += row.Clicks
		acc.impressions += row.Impressions
		acc.weightedPosSum += row.Position * row.Impressions

		acc.queries = append(acc.queries, model.QueryPerf{
			Query:       query,
			Clicks:      row.Clicks,
			Impressions: row.Impressions,
			CTR:         row.CTR,
			Position:    row.Position,
		})
	}

	result := make(map[string]*model.PagePerf)

	for pageURL, acc := range accMap {
		avgPos := 0.0
		ctr := 0.0
		if acc.impressions > 0 {
			avgPos = acc.weightedPosSum / acc.impressions
			ctr = acc.clicks / acc.impressions
		}

		// Urutkan queries per halaman: clicks terbanyak, lalu impressions terbanyak
		sort.SliceStable(acc.queries, func(i, j int) bool {
			if acc.queries[i].Clicks != acc.queries[j].Clicks {
				return acc.queries[i].Clicks > acc.queries[j].Clicks
			}
			return acc.queries[i].Impressions > acc.queries[j].Impressions
		})

		result[pageURL] = &model.PagePerf{
			Clicks:      acc.clicks,
			Impressions: acc.impressions,
			CTR:         ctr,
			Position:    avgPos,
			Page1:       avgPos > 0 && avgPos <= 10.0,
			Queries:     acc.queries,
		}
	}

	return result
}
