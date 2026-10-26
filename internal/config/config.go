package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	ServiceAccountFile string   `json:"service_account_file"`
	DataDir            string   `json:"data_dir"`
	Defaults           Defaults `json:"defaults"`
	Sites              []Site   `json:"sites"`
	Notify             Notify   `json:"notify"`
}

type Defaults struct {
	UserAgent          string   `json:"user_agent"`
	CrawlConcurrency   int      `json:"crawl_concurrency"`
	CrawlDelayMS       int      `json:"crawl_delay_ms"`
	CrawlTimeoutS      int      `json:"crawl_timeout_s"`
	MaxPages           int      `json:"max_pages"`
	InspectRPM         int      `json:"inspect_rpm"`
	InspectDailyBudget int      `json:"inspect_daily_budget"`
	ReinspectAfterDays int      `json:"reinspect_after_days"`
	PerfDays           int      `json:"perf_days"`
	Stopwords          []string `json:"stopwords"`
}

type Site struct {
	Name          string   `json:"name"`
	GSCProperty   string   `json:"gsc_property"`
	BaseURL       string   `json:"base_url"`
	Sitemaps      []string `json:"sitemaps"`
	ExcludePaths  []string `json:"exclude_paths"`
	PriorityPaths []string `json:"priority_paths"`
	IndexNowKey   string   `json:"indexnow_key"`
}

type Notify struct {
	TelegramTokenEnv  string `json:"telegram_token_env"`
	TelegramChatID    string `json:"telegram_chat_id"`
	DiscordWebhookEnv string `json:"discord_webhook_env"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Load membaca, mengisi default, dan memvalidasi config.
// Path relatif dihitung dari folder file config, bukan dari CWD.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields() // menolak secret yang ditulis langsung di config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config tidak valid: %w", err)
	}
	c.applyDefaults()

	base := filepath.Dir(path)
	c.ServiceAccountFile = resolve(base, c.ServiceAccountFile)
	c.DataDir = resolve(base, c.DataDir)

	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func resolve(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func (c *Config) applyDefaults() {
	d := &c.Defaults
	setInt := func(v *int, def int) {
		if *v == 0 {
			*v = def
		}
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	setInt(&d.CrawlConcurrency, 5)
	setInt(&d.CrawlDelayMS, 200)
	setInt(&d.CrawlTimeoutS, 20)
	setInt(&d.MaxPages, 5000)
	setInt(&d.InspectRPM, 300)
	setInt(&d.InspectDailyBudget, 1800)
	setInt(&d.ReinspectAfterDays, 7)
	setInt(&d.PerfDays, 28)
}

func (c *Config) validate() error {
	var errs []error
	add := func(f string, a ...any) { errs = append(errs, fmt.Errorf(f, a...)) }

	if err := checkServiceAccount(c.ServiceAccountFile); err != nil {
		add("service_account_file: %v", err)
	}
	if c.Defaults.InspectDailyBudget < 1 || c.Defaults.InspectDailyBudget > 2000 {
		add("inspect_daily_budget harus 1..2000 (batas Google), dapat %d", c.Defaults.InspectDailyBudget)
	}
	if c.Defaults.InspectRPM < 1 || c.Defaults.InspectRPM > 600 {
		add("inspect_rpm harus 1..600 (batas Google), dapat %d", c.Defaults.InspectRPM)
	}
	if len(c.Sites) == 0 {
		add("sites: minimal 1 site")
	}
	seen := map[string]bool{}
	for i, s := range c.Sites {
		p := fmt.Sprintf("sites[%d]", i)
		switch {
		case !nameRe.MatchString(s.Name):
			add("%s.name %q harus [a-z0-9_-]+", p, s.Name)
		case seen[s.Name]:
			add("%s.name %q duplikat", p, s.Name)
		}
		seen[s.Name] = true
		if !validProperty(s.GSCProperty) {
			add("%s.gsc_property %q harus 'sc-domain:host' atau 'http(s)://host/' berakhiran '/'", p, s.GSCProperty)
		}
		if !validBaseURL(s.BaseURL) {
			add("%s.base_url %q harus http(s)://host tanpa path", p, s.BaseURL)
		}
	}
	return errors.Join(errs...)
}

func checkServiceAccount(path string) error {
	if path == "" {
		return errors.New("wajib diisi")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(b, &sa); err != nil {
		return fmt.Errorf("bukan JSON valid: %w", err)
	}
	var missing []string
	for k, v := range map[string]string{"client_email": sa.ClientEmail, "private_key": sa.PrivateKey, "token_uri": sa.TokenURI} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("field kosong: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validProperty(p string) bool {
	if h, ok := strings.CutPrefix(p, "sc-domain:"); ok {
		return h != "" && !strings.ContainsAny(h, "/ ")
	}
	u, err := url.Parse(p)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && strings.HasSuffix(p, "/")
}

func validBaseURL(b string) bool {
	u, err := url.Parse(b)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		(u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}
