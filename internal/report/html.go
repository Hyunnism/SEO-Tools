package report

import (
	"html/template"
	"io"
	"sort"
	"strings"

	"seo-boost/internal/model"
)

const htmlReportTemplate = `<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>SEO Report: {{.Site}}</title>
  <style>
    :root {
      --bg: #f8fafc;
      --card-bg: #ffffff;
      --text: #0f172a;
      --text-muted: #64748b;
      --border: #e2e8f0;
      --primary: #0284c7;
      --ok-bg: #dcfce7;
      --ok-text: #166534;
      --warn-bg: #fef9c3;
      --warn-text: #854d0e;
      --err-bg: #fee2e2;
      --err-text: #991b1b;
      --badge-neutral-bg: #f1f5f9;
      --badge-neutral-text: #475569;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      background-color: var(--bg);
      color: var(--text);
      line-height: 1.5;
      padding: 2rem 1rem;
    }
    .container {
      max-width: 1200px;
      margin: 0 auto;
    }
    header {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 1.5rem 2rem;
      margin-bottom: 2rem;
    }
    header h1 {
      font-size: 1.75rem;
      color: var(--text);
      margin-bottom: 0.5rem;
    }
    .meta-bar {
      display: flex;
      flex-wrap: wrap;
      gap: 1.5rem;
      color: var(--text-muted);
      font-size: 0.875rem;
    }
    section {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 1.5rem 2rem;
      margin-bottom: 2rem;
    }
    section h2 {
      font-size: 1.25rem;
      margin-bottom: 1rem;
      color: var(--text);
      border-bottom: 2px solid var(--border);
      padding-bottom: 0.5rem;
    }
    section h3 {
      font-size: 1rem;
      margin: 1.25rem 0 0.5rem 0;
      color: var(--text-muted);
    }
    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 1rem;
      margin-bottom: 1rem;
    }
    .stat-card {
      background: var(--badge-neutral-bg);
      border: 1px solid var(--border);
      border-radius: 6px;
      padding: 1rem;
      text-align: center;
    }
    .stat-card .val {
      font-size: 1.75rem;
      font-weight: 700;
      color: var(--text);
    }
    .stat-card .lbl {
      font-size: 0.8125rem;
      color: var(--text-muted);
      margin-top: 0.25rem;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 0.875rem;
      margin-top: 0.5rem;
    }
    th, td {
      padding: 0.625rem 0.75rem;
      text-align: left;
      border-bottom: 1px solid var(--border);
    }
    th {
      background-color: var(--badge-neutral-bg);
      font-weight: 600;
      color: var(--text);
    }
    tr:hover {
      background-color: #f8fafc;
    }
    .badge {
      display: inline-block;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 0.75rem;
      font-weight: 600;
      padding: 0.125rem 0.375rem;
      border-radius: 4px;
      text-transform: uppercase;
    }
    .badge-ok { background: var(--ok-bg); color: var(--ok-text); }
    .badge-warn { background: var(--warn-bg); color: var(--warn-text); }
    .badge-err { background: var(--err-bg); color: var(--err-text); }
    .badge-info { background: var(--badge-neutral-bg); color: var(--badge-neutral-text); }
    .badge-p1 { background: #e0e7ff; color: #3730a3; }
    .badge-boost { background: #fef08a; color: #854d0e; }
    .url-text {
      word-break: break-all;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 0.8125rem;
    }
    .delta-pos { color: #15803d; font-weight: 600; }
    .delta-neg { color: #b91c1c; font-weight: 600; }
    @media print {
      body { background: #ffffff; padding: 0; }
      header, section { border: none; box-shadow: none; padding: 1rem 0; }
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>SEO Audit & Performance Report: {{.Site}}</h1>
      <div class="meta-bar">
        <span>Waktu: <strong>{{.Timestamp.Format "2006-01-02 15:04:05 MST"}}</strong></span>
        {{if .GSCProperty}}<span>GSC Property: <strong>{{.GSCProperty}}</strong></span>{{end}}
      </div>
    </header>

    {{if .Summary}}
    <section>
      <h2>1. Ringkasan Eksekutif</h2>
      <div class="stats-grid">
        <div class="stat-card">
          <div class="val">{{.Summary.TotalPages}}</div>
          <div class="lbl">Total Halaman</div>
        </div>
        <div class="stat-card">
          <div class="val" style="color: var(--ok-text);">{{.Summary.IndexedCount}}</div>
          <div class="lbl">Terindeks [OK]</div>
        </div>
        <div class="stat-card">
          <div class="val" style="color: var(--warn-text);">{{.Summary.TroubledCount}}</div>
          <div class="lbl">Bermasalah [WARN]</div>
        </div>
        <div class="stat-card">
          <div class="val" style="color: #4338ca;">{{.Summary.Page1Count}}</div>
          <div class="lbl">Halaman 1 Google</div>
        </div>
        <div class="stat-card">
          <div class="val">{{printf "%.0f" .Summary.TotalClicks}}</div>
          <div class="lbl">Total Clicks</div>
        </div>
        <div class="stat-card">
          <div class="val">{{printf "%.0f" .Summary.TotalImpressions}}</div>
          <div class="lbl">Total Impressions</div>
        </div>
        {{if gt .Summary.AveragePosition 0.0}}
        <div class="stat-card">
          <div class="val">{{printf "%.2f" .Summary.AveragePosition}}</div>
          <div class="lbl">Rata-rata Posisi</div>
        </div>
        {{end}}
      </div>
    </section>
    {{end}}

    {{if and .Diff .Diff.HasPrevRun}}
    <section>
      <h2>2. Perubahan vs Run Sebelumnya (Diff)</h2>
      <div class="stats-grid">
        <div class="stat-card">
          <div class="val {{if ge .Diff.DeltaClicks 0.0}}delta-pos{{else}}delta-neg{{end}}">
            {{printf "%+.0f" .Diff.DeltaClicks}}
          </div>
          <div class="lbl">Delta Clicks</div>
        </div>
        <div class="stat-card">
          <div class="val {{if ge .Diff.DeltaImpressions 0.0}}delta-pos{{else}}delta-neg{{end}}">
            {{printf "%+.0f" .Diff.DeltaImpressions}}
          </div>
          <div class="lbl">Delta Impressions</div>
        </div>
        {{if ne .Diff.DeltaAvgPosition 0.0}}
        <div class="stat-card">
          <div class="val {{if le .Diff.DeltaAvgPosition 0.0}}delta-pos{{else}}delta-neg{{end}}">
            {{printf "%+.2f" .Diff.DeltaAvgPosition}}
          </div>
          <div class="lbl">Delta Posisi</div>
        </div>
        {{end}}
      </div>

      {{if .Diff.NewIndexed}}
      <h3>[+] Halaman Baru Terindeks ({{len .Diff.NewIndexed}})</h3>
      <table>
        <thead><tr><th>URL</th></tr></thead>
        <tbody>
          {{range .Diff.NewIndexed}}
          <tr><td class="url-text"><span class="badge badge-ok">[+]</span> {{.}}</td></tr>
          {{end}}
        </tbody>
      </table>
      {{end}}

      {{if .Diff.DeIndexed}}
      <h3>[-] Halaman Hilang dari Indeks ({{len .Diff.DeIndexed}})</h3>
      <table>
        <thead><tr><th>URL</th></tr></thead>
        <tbody>
          {{range .Diff.DeIndexed}}
          <tr><td class="url-text"><span class="badge badge-err">[-]</span> {{.}}</td></tr>
          {{end}}
        </tbody>
      </table>
      {{end}}

      {{if .Diff.RankGains}}
      <h3>[UP] Kenaikan Peringkat Signifikan (&gt;= 3 Posisi, Total: {{len .Diff.RankGains}})</h3>
      <table>
        <thead><tr><th>URL</th><th>Posisi Lama</th><th>Posisi Baru</th><th>Perubahan</th></tr></thead>
        <tbody>
          {{range .Diff.RankGains}}
          <tr>
            <td class="url-text">{{.URL}}</td>
            <td>{{printf "%.1f" .OldPosition}}</td>
            <td>{{printf "%.1f" .NewPosition}}</td>
            <td class="delta-pos">{{printf "%+.1f" (negShift .Shift)}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      {{end}}

      {{if .Diff.RankDrops}}
      <h3>[DOWN] Penurunan Peringkat Signifikan (&gt;= 3 Posisi, Total: {{len .Diff.RankDrops}})</h3>
      <table>
        <thead><tr><th>URL</th><th>Posisi Lama</th><th>Posisi Baru</th><th>Perubahan</th></tr></thead>
        <tbody>
          {{range .Diff.RankDrops}}
          <tr>
            <td class="url-text">{{.URL}}</td>
            <td>{{printf "%.1f" .OldPosition}}</td>
            <td>{{printf "%.1f" .NewPosition}}</td>
            <td class="delta-neg">{{printf "%+.1f" .Shift}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      {{end}}
    </section>
    {{end}}

    {{if .TopGaps}}
    <section>
      <h2>3. Peluang Keyword Gap Teratas</h2>
      <table>
        <thead>
          <tr>
            <th>Skor</th>
            <th>Kategori</th>
            <th>Kata Kunci</th>
            <th>Posisi</th>
            <th>Impresi</th>
            <th>CTR</th>
            <th>Match</th>
          </tr>
        </thead>
        <tbody>
          {{range .TopGaps}}
          <tr>
            <td><strong>{{printf "%.1f" .Score}}</strong></td>
            <td><span class="badge badge-boost">[{{.Category}}]</span></td>
            <td><strong>{{.Query}}</strong></td>
            <td>{{printf "%.1f" .Position}}</td>
            <td>{{printf "%.0f" .Impressions}}</td>
            <td>{{printf "%.1f%%" (mul100 .CTR)}}</td>
            <td><code>{{.Match}}</code></td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </section>
    {{end}}

    {{if .AuditIssues}}
    <section>
      <h2>4. Temuan Audit On-Page</h2>
      <table>
        <thead>
          <tr>
            <th>Tingkat</th>
            <th>Kode</th>
            <th>Keterangan</th>
            <th>URL</th>
          </tr>
        </thead>
        <tbody>
          {{range .AuditIssues}}
          <tr>
            <td>
              {{if eq .Severity "error"}}<span class="badge badge-err">[ERROR]</span>
              {{else if eq .Severity "warn"}}<span class="badge badge-warn">[WARN]</span>
              {{else}}<span class="badge badge-info">[{{.Severity}}]</span>{{end}}
            </td>
            <td><code>{{.Code}}</code></td>
            <td>{{.Detail}}</td>
            <td class="url-text">{{if .URL}}{{.URL}}{{else}}-{{end}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </section>
    {{end}}

    {{if .Pages}}
    <section>
      <h2>5. Rincian Halaman</h2>
      <table>
        <thead>
          <tr>
            <th>Status Index</th>
            <th>HTTP</th>
            <th>URL</th>
            <th>Judul Halaman</th>
            <th>Clicks</th>
            <th>Impresi</th>
            <th>Posisi</th>
          </tr>
        </thead>
        <tbody>
          {{range .Pages}}
          <tr>
            <td>
              {{if .Index}}
                {{if eq (indexStatusStr .Index.Status) "INDEXED"}}<span class="badge badge-ok">[INDEXED]</span>
                {{else}}<span class="badge badge-warn">[{{indexStatusStr .Index.Status}}]</span>{{end}}
              {{else}}
                <span class="badge badge-info">[UNINSPECTED]</span>
              {{end}}
            </td>
            <td>{{if .Crawl}}{{.Crawl.HTTPStatus}}{{else}}0{{end}}</td>
            <td class="url-text">{{.URL}}</td>
            <td>{{if and .Crawl .Crawl.Title}}{{.Crawl.Title}}{{else}}-{{end}}</td>
            <td>{{if .Perf}}{{printf "%.0f" .Perf.Clicks}}{{if and .Perf.Delta (ne .Perf.Delta.Clicks 0.0)}} ({{printf "%+.0f" .Perf.Delta.Clicks}}){{end}}{{else}}0{{end}}</td>
            <td>{{if .Perf}}{{printf "%.0f" .Perf.Impressions}}{{if and .Perf.Delta (ne .Perf.Delta.Impressions 0.0)}} ({{printf "%+.0f" .Perf.Delta.Impressions}}){{end}}{{else}}0{{end}}</td>
            <td>{{if .Perf}}{{printf "%.1f" .Perf.Position}}{{if and .Perf.Delta (lt .Perf.Delta.Position -0.05)}} [UP {{printf "%.1f" (negShift .Perf.Delta.Position)}}]{{else if and .Perf.Delta (gt .Perf.Delta.Position 0.05)}} [DOWN {{printf "%.1f" .Perf.Delta.Position}}]{{end}}{{else}}0.0{{end}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </section>
    {{end}}
  </div>
</body>
</html>
`

// RenderHTML menuliskan laporan SEO dalam format berkas mandiri HTML dengan inline CSS dan anti-XSS auto-escaping.
func RenderHTML(w io.Writer, data *ReportData) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"negShift": func(v float64) float64 {
			return -v
		},
		"mul100": func(v float64) float64 {
			return v * 100
		},
		"indexStatusStr": func(st model.IndexStatus) string {
			return strings.ToUpper(string(st))
		},
	}).Parse(htmlReportTemplate)
	if err != nil {
		return err
	}

	// Buat salinan terurut untuk pages (bermasalah di atas)
	dataCopy := *data
	if len(data.Pages) > 0 {
		sortedPages := make([]*model.Page, len(data.Pages))
		copy(sortedPages, data.Pages)
		sort.Slice(sortedPages, func(i, j int) bool {
			iTroubled := isTroubledPage(sortedPages[i])
			jTroubled := isTroubledPage(sortedPages[j])
			if iTroubled != jTroubled {
				return iTroubled
			}
			return sortedPages[i].URL < sortedPages[j].URL
		})
		dataCopy.Pages = sortedPages
	}

	return tmpl.Execute(w, &dataCopy)
}
