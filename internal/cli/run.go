package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/report"
	"seo-boost/internal/store"
)

func runOrchestrator(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)

	spider := true
	concurrency := 0
	maxPages := 100
	days := 28
	limit := 50
	minImp := 50
	dryRun := false
	force := false
	failOnIssues := false

	fs.BoolVar(&spider, "spider", true, "sertakan internal spider BFS untuk melengkapi sitemap")
	fs.IntVar(&concurrency, "concurrency", 0, "jumlah worker paralel crawling")
	fs.IntVar(&maxPages, "max-pages", 100, "maksimal total halaman yang dirayapi")
	fs.IntVar(&days, "days", 28, "rentang hari data Search Analytics GSC")
	fs.IntVar(&limit, "limit", 50, "maksimal URL yang diinspeksi ke Google")
	fs.IntVar(&minImp, "min-imp", 50, "ambang minimum impresi untuk keyword gap")
	fs.BoolVar(&dryRun, "dry-run", false, "simulasikan inspect tanpa memanggil API Google")
	fs.BoolVar(&force, "force", false, "paksa inspeksi ulang mengabaikan batas reinspect_after_days")
	fs.BoolVar(&failOnIssues, "fail-on-issues", false, "kembalikan exit code 2 bila ditemukan masalah kritis")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Muat config: %v\n", err)
		return 1
	}

	site, err := resolveSite(c, g.site)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] %v\n", err)
		return 1
	}

	log := newLogger(stderr, g.verbose)
	st := store.NewStore(c.DataDir)

	// Kunci akses eksklusif untuk site ini
	unlock, err := st.Lock(site.Name)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Mengunci store untuk site %s: %v\n", site.Name, err)
		return 1
	}
	defer unlock()

	fmt.Fprintf(stdout, "================================================================================\n")
	fmt.Fprintf(stdout, "  SEO-BOOST PIPELINE: %s (%s)\n", site.Name, site.BaseURL)
	fmt.Fprintf(stdout, "================================================================================\n\n")

	ctx := context.Background()
	opts := pipelineOptions{
		spider:      spider,
		concurrency: concurrency,
		maxPages:    maxPages,
		days:        days,
		limit:       limit,
		minImp:      minImp,
		dryRun:      dryRun,
		force:       force,
	}

	pages, currSummary, diff, allGaps, err := executePipeline(ctx, c, site, opts, log, st, nil, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "[GAGAL] Eksekusi pipeline: %v\n", err)
		return 1
	}

	// -------------------------------------------------------------------------
	// LAPORAN AKHIR
	// -------------------------------------------------------------------------
	topGaps := allGaps
	if len(topGaps) > 20 {
		topGaps = topGaps[:20]
	}

	var reportedIssues []report.ReportedIssue
	for _, p := range pages {
		for _, iss := range p.Audit {
			reportedIssues = append(reportedIssues, report.ReportedIssue{
				URL:      p.URL,
				Code:     iss.Code,
				Severity: iss.Severity,
				Detail:   iss.Detail,
			})
		}
	}

	repData := &report.ReportData{
		Site:        site.Name,
		GSCProperty: site.GSCProperty,
		Timestamp:   time.Now().UTC(),
		Summary:     currSummary,
		Diff:        diff,
		TopGaps:     topGaps,
		AuditIssues: reportedIssues,
		Pages:       pages,
	}

	var outBuf bytes.Buffer
	switch strings.ToLower(g.format) {
	case "json":
		if err := report.RenderJSON(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render JSON: %v\n", err)
			return 1
		}
	case "md", "markdown":
		if err := report.RenderMarkdown(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render Markdown: %v\n", err)
			return 1
		}
	case "html":
		if err := report.RenderHTML(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render HTML: %v\n", err)
			return 1
		}
	default:
		if err := report.RenderText(&outBuf, repData); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Render report: %v\n", err)
			return 1
		}
	}

	outputBytes := outBuf.Bytes()
	fmt.Fprint(stdout, string(outputBytes))

	if g.out != "" {
		if err := os.WriteFile(g.out, outputBytes, 0o644); err != nil {
			fmt.Fprintf(stderr, "[GAGAL] Tulis file output %s: %v\n", g.out, err)
			return 1
		}
		fmt.Fprintf(stdout, "\nLaporan lengkap tersimpan di: %s\n", g.out)
	}

	if failOnIssues && report.HasCriticalIssues(pages, reportedIssues, site.PriorityPaths) {
		fmt.Fprintf(stderr, "\n[PERINGATAN] Ditemukan masalah kritis SEO pada situs %s (--fail-on-issues aktif: exit code 2).\n", site.Name)
		return 2
	}

	return 0
}
