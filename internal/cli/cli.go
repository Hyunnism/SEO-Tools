package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"seo-boost/internal/config"
)

const usage = `seo-boost - SEO automation tool

Pemakaian: seo-boost <perintah> [flag]

Perintah tersedia:
  version   tampilkan versi
  doctor    validasi config
  help      tampilkan bantuan ini

Flag global: --config --site --format --out --verbose
`

type global struct {
	config, site, format, out string
	verbose                   bool
}

func globalFlags(fs *flag.FlagSet) *global {
	g := &global{}
	fs.StringVar(&g.config, "config", "./config.json", "path ke config.json")
	fs.StringVar(&g.site, "site", "", `nama site di config ("all" = semua)`)
	fs.StringVar(&g.format, "format", "text", "text | json | md | html")
	fs.StringVar(&g.out, "out", "", "tulis laporan ke file")
	fs.BoolVar(&g.verbose, "verbose", false, "log detail")
	return g
}

func newLogger(w io.Writer, verbose bool) *slog.Logger {
	lvl := slog.LevelInfo
	if verbose {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}

// Run mengembalikan exit code: 0 sukses, 1 error runtime/penggunaan.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "doctor":
		return doctor(rest, stdout, stderr)
	case "discover", "crawl", "inspect", "perf", "gap", "submit", "run", "report", "serve", "indexnow-key":
		fmt.Fprintf(stderr, "perintah %q belum tersedia\n", cmd)
		return 1
	default:
		fmt.Fprintf(stderr, "perintah tidak dikenal: %q\n\n%s", cmd, usage)
		return 1
	}
}

func doctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := globalFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	log := newLogger(stderr, g.verbose)
	log.Debug("memuat config", "path", g.config)
	c, err := config.Load(g.config)
	if err != nil {
		fmt.Fprintln(stderr, "Config GAGAL:")
		fmt.Fprintln(stderr, strings.TrimSpace(err.Error()))
		return 1
	}
	fmt.Fprintf(stdout, "Config OK: %d site\n", len(c.Sites))
	return 0
}
