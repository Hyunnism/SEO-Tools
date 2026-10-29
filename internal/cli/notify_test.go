package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"


	"seo-boost/internal/model"
	"seo-boost/internal/store"
)

func TestNotifyCmd_Help(t *testing.T) {
	code, out, errs := run("notify", "--help")
	if code != 0 {
		t.Fatalf("expected exit code 0 for help, got: %d", code)
	}
	combined := out + errs
	if !strings.Contains(combined, "-always") {
		t.Errorf("expected help output to mention -always, got: %s", combined)
	}
}

func TestNotifyCmd_NoRunsFound(t *testing.T) {
	dir := t.TempDir()
	saPath := filepath.Join(dir, "sa.json")
	_ = os.WriteFile(saPath, []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o644)

	cfgPath := filepath.Join(dir, "config.json")
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "%s",
		"data_dir": "%s",
		"sites": [{"name": "site-test", "base_url": "https://test.com", "gsc_property": "sc-domain:test.com"}]
	}`, filepath.ToSlash(saPath), filepath.ToSlash(filepath.Join(dir, "data")))
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)

	code, _, errs := run("notify", "--config", cfgPath, "--site", "site-test")
	if code != 1 {
		t.Fatalf("expected exit code 1 when no runs exist, got: %d", code)
	}
	if !strings.Contains(errs, "Belum ada riwayat run") {
		t.Errorf("expected error message about no runs, got: %s", errs)
	}
}

func TestNotifyCmd_NoiseReductionAndAlways(t *testing.T) {
	dir := t.TempDir()
	saPath := filepath.Join(dir, "sa.json")
	_ = os.WriteFile(saPath, []byte(`{"client_email":"a@b.com","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o644)

	dataDir := filepath.Join(dir, "data")
	st := store.NewStore(dataDir)

	// Inisialisasi mock run
	pages := []*model.Page{
		{
			URL: "https://test.com/",
			Index: &model.IndexInfo{
				Status: model.StatusIndexed,
			},
		},
	}
	_ = st.SavePages("site-test", pages)
	summary := store.BuildRunSummary("site-test", pages)
	_ = st.SaveRun("site-test", summary)

	var discordReceived bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordReceived = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	os.Setenv("TEST_DISCORD_NOTIFY_HOOK", ts.URL)
	defer os.Unsetenv("TEST_DISCORD_NOTIFY_HOOK")

	cfgPath := filepath.Join(dir, "config.json")
	cfgContent := fmt.Sprintf(`{
		"service_account_file": "%s",
		"data_dir": "%s",
		"sites": [{"name": "site-test", "base_url": "https://test.com", "gsc_property": "sc-domain:test.com"}],
		"notify": {
			"discord_webhook_env": "TEST_DISCORD_NOTIFY_HOOK"
		}
	}`, filepath.ToSlash(saPath), filepath.ToSlash(dataDir))
	_ = os.WriteFile(cfgPath, []byte(cfgContent), 0o644)

	// 1. Tanpa --always, tidak ada perubahan bermakna -> dilewati (noise reduction)
	code, out, _ := run("notify", "--config", cfgPath, "--site", "site-test")
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(out, "dilewati") {
		t.Errorf("expected output to mention skipped notification, got: %s", out)
	}
	if discordReceived {
		t.Errorf("discord webhook should not be called when skipped")
	}

	// 2. Dengan --always -> dipaksa kirim
	codeAlways, outAlways, _ := run("notify", "--config", cfgPath, "--site", "site-test", "--always")
	if codeAlways != 0 {
		t.Fatalf("expected code 0 for --always, got %d", codeAlways)
	}
	if !strings.Contains(outAlways, "[OK]") {
		t.Errorf("expected output to indicate successful notification, got: %s", outAlways)
	}
	if !discordReceived {
		t.Errorf("discord webhook should be called with --always")
	}
}
