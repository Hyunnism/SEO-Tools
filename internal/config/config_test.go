package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSA = `{"client_email":"a@b.iam","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`

// write membuat dir sementara berisi sa.json dan config.json; mengembalikan path config.
func write(t *testing.T, sa, cfg string) string {
	t.Helper()
	dir := t.TempDir()
	if sa != "" {
		if err := os.WriteFile(filepath.Join(dir, "sa.json"), []byte(sa), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func site(name, prop, base string) string {
	return `{"name":"` + name + `","gsc_property":"` + prop + `","base_url":"` + base + `"}`
}

func cfgWith(extra string, sites ...string) string {
	return `{"service_account_file":"./sa.json"` + extra + `,"sites":[` + strings.Join(sites, ",") + `]}`
}

const okSite = `{"name":"portfolio","gsc_property":"sc-domain:example.com","base_url":"https://example.com"}`

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		sa      string
		cfg     string
		wantErr string // kosong = harus lolos
	}{
		{"valid", goodSA, cfgWith("", okSite), ""},
		{"valid url-prefix", goodSA, cfgWith("", site("a", "https://example.com/", "http://example.com/")), ""},
		{"nama duplikat", goodSA, cfgWith("", okSite, okSite), "duplikat"},
		{"nama huruf besar", goodSA, cfgWith("", site("Portfolio", "sc-domain:e.com", "https://e.com")), "[a-z0-9_-]+"},
		{"nama spasi", goodSA, cfgWith("", site("my site", "sc-domain:e.com", "https://e.com")), "[a-z0-9_-]+"},
		{"nama traversal", goodSA, cfgWith("", site("../x", "sc-domain:e.com", "https://e.com")), "[a-z0-9_-]+"},
		{"property tanpa prefix", goodSA, cfgWith("", site("a", "example.com", "https://e.com")), "gsc_property"},
		{"property tanpa slash", goodSA, cfgWith("", site("a", "https://example.com", "https://e.com")), "gsc_property"},
		{"property ftp", goodSA, cfgWith("", site("a", "ftp://example.com/", "https://e.com")), "gsc_property"},
		{"base_url ber-path", goodSA, cfgWith("", site("a", "sc-domain:e.com", "https://e.com/blog")), "base_url"},
		{"base_url ftp", goodSA, cfgWith("", site("a", "sc-domain:e.com", "ftp://e.com")), "base_url"},
		{"base_url tanpa host", goodSA, cfgWith("", site("a", "sc-domain:e.com", "https://")), "base_url"},
		{"budget 2001", goodSA, cfgWith(`,"defaults":{"inspect_daily_budget":2001}`, okSite), "inspect_daily_budget"},
		{"rpm 601", goodSA, cfgWith(`,"defaults":{"inspect_rpm":601}`, okSite), "inspect_rpm"},
		{"tanpa site", goodSA, cfgWith(""), "minimal 1 site"},
		{"sa tidak ada", "", cfgWith("", okSite), "service_account_file"},
		{"sa bukan json", "bukan json", cfgWith("", okSite), "bukan JSON"},
		{"sa tanpa client_email", `{"private_key":"k","token_uri":"u"}`, cfgWith("", okSite), "client_email"},
		{"sa tanpa private_key", `{"client_email":"e","token_uri":"u"}`, cfgWith("", okSite), "private_key"},
		{"sa tanpa token_uri", `{"client_email":"e","private_key":"k"}`, cfgWith("", okSite), "token_uri"},
		{"secret di config ditolak", goodSA, cfgWith(`,"notify":{"telegram_token":"abc"}`, okSite), "unknown field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, tc.sa, tc.cfg))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("harusnya lolos: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, mau memuat %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(write(t, goodSA, cfgWith("", okSite)))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Defaults
	if d.CrawlConcurrency != 5 || d.CrawlDelayMS != 200 || d.CrawlTimeoutS != 20 || d.MaxPages != 5000 ||
		d.InspectRPM != 300 || d.InspectDailyBudget != 1800 || d.ReinspectAfterDays != 7 || d.PerfDays != 28 {
		t.Fatalf("default salah: %+v", d)
	}
	if !strings.HasSuffix(filepath.ToSlash(c.DataDir), "/data") {
		t.Fatalf("data_dir default salah: %q", c.DataDir)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(write(t, goodSA, cfgWith(`,"defaults":{"inspect_rpm":999}`, site("A", "x", "y"))))
	if err == nil {
		t.Fatal("harusnya error")
	}
	for _, want := range []string{"inspect_rpm", "name", "gsc_property", "base_url"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error tidak memuat %q: %v", want, err)
		}
	}
}

func TestRelativePathFromConfigDir(t *testing.T) {
	p := write(t, goodSA, cfgWith("", okSite))
	t.Chdir(t.TempDir()) // CWD sengaja berbeda dari folder config
	c, err := Load(p)
	if err != nil {
		t.Fatalf("path relatif harus dihitung dari folder config: %v", err)
	}
	if want := filepath.Join(filepath.Dir(p), "sa.json"); c.ServiceAccountFile != want {
		t.Fatalf("got %q want %q", c.ServiceAccountFile, want)
	}
}

func TestExampleConfigParses(t *testing.T) {
	b, err := os.ReadFile("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets", "sa.json"), []byte(goodSA), 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("config.example.json harus lolos: %v", err)
	}
}
