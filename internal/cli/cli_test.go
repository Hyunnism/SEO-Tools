package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, "dev")
	return code, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := run("version")
	if code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestNoArgs(t *testing.T) {
	code, _, errs := run()
	if code != 1 || !strings.Contains(errs, "Pemakaian") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errs := run("ngawur")
	if code != 1 || !strings.Contains(errs, "ngawur") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestNotYetAvailable(t *testing.T) {
	code, _, errs := run("crawl")
	if code != 1 || !strings.Contains(errs, "belum tersedia") {
		t.Fatalf("code=%d err=%q", code, errs)
	}
}

func TestHelp(t *testing.T) {
	for _, a := range []string{"help", "--help", "-h"} {
		code, out, _ := run(a)
		if code != 0 || !strings.Contains(out, "Pemakaian") {
			t.Fatalf("%s: code=%d out=%q", a, code, out)
		}
	}
}

func TestDoctor(t *testing.T) {
	dir := t.TempDir()
	sa := `{"client_email":"a","private_key":"k","token_uri":"u"}`
	good := `{"service_account_file":"./sa.json","sites":[{"name":"a","gsc_property":"sc-domain:e.com","base_url":"https://e.com"}]}`
	bad := `{"service_account_file":"./sa.json","sites":[]}`
	for name, body := range map[string]string{"sa.json": sa, "good.json": good, "bad.json": bad} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if code, out, _ := run("doctor", "--config", filepath.Join(dir, "good.json")); code != 0 || !strings.Contains(out, "Config OK: 1 site") {
		t.Fatalf("good: code=%d out=%q", code, out)
	}
	if code, _, errs := run("doctor", "--config", filepath.Join(dir, "bad.json")); code != 1 || !strings.Contains(errs, "minimal 1 site") {
		t.Fatalf("bad: code=%d err=%q", code, errs)
	}
	if code, _, errs := run("doctor", "--config", filepath.Join(dir, "tidak-ada.json")); code != 1 || errs == "" {
		t.Fatalf("missing: code=%d err=%q", code, errs)
	}
}

func TestVerboseLogger(t *testing.T) {
	var b bytes.Buffer
	if newLogger(&b, false).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus mati tanpa --verbose")
	}
	if !newLogger(&b, true).Enabled(nil, slog.LevelDebug) {
		t.Fatal("debug harus nyala dengan --verbose")
	}
}
