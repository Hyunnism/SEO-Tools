package model

import "time"

// IndexStatus mewakili status index halaman yang dinormalisasi.
type IndexStatus string

const (
	StatusIndexed              IndexStatus = "indexed"
	StatusCrawledNotIndexed    IndexStatus = "crawled_not_indexed"
	StatusDiscoveredNotIndexed IndexStatus = "discovered_not_indexed"
	StatusUnknownToGoogle      IndexStatus = "unknown_to_google"
	StatusBlocked              IndexStatus = "blocked"   // robots.txt / noindex
	StatusError                IndexStatus = "error"     // 404 / 5xx / soft 404 / redirect error
	StatusDuplicate            IndexStatus = "duplicate" // canonical lain dipilih
	StatusOther                IndexStatus = "other"     // tampilkan RawCoverage
	StatusNeverInspected       IndexStatus = "never_inspected"
)

// MatchLevel mewakili level kecocokan kata kunci pada teks halaman.
type MatchLevel string

const (
	MatchExact   MatchLevel = "exact"     // frasa utuh ada di teks
	MatchAllWord MatchLevel = "all_words" // semua kata ada tapi tidak berurutan
	MatchPartial MatchLevel = "partial"   // sebagian kata hilang
	MatchAbsent  MatchLevel = "absent"    // tidak ada sama sekali
)

// Zones menampung teks halaman per zona SEO (tidak disimpan ke JSON untuk hemat ruang).
type Zones struct {
	Title string   `json:"-"`
	Meta  string   `json:"-"`
	URL   string   `json:"-"`
	H1    []string `json:"-"`
	H2H3  []string `json:"-"`
	Body  []string `json:"-"`
	Alt   []string `json:"-"`
}

// AuditIssue mewakili satu temuan audit SEO on-page.
type AuditIssue struct {
	Code     string `json:"code"`     // misal: title_too_long, h1_missing
	Severity string `json:"severity"` // error | warn | info
	Detail   string `json:"detail"`
}

// CrawlInfo menyimpan hasil crawling dan ekstraksi on-page suatu URL.
type CrawlInfo struct {
	FetchedAt   time.Time `json:"fetched_at"`
	HTTPStatus  int       `json:"http_status"`
	FinalURL    string    `json:"final_url"`
	Title       string    `json:"title"`
	MetaDesc    string    `json:"meta_description"`
	Canonical   string    `json:"canonical"`
	RobotsMeta  string    `json:"robots_meta"`
	H1          []string  `json:"h1"`
	H2H3        []string  `json:"h2_h3"`
	WordCount   int       `json:"word_count"`
	ImagesNoAlt int       `json:"images_no_alt"`
	HasJSONLD   bool      `json:"has_jsonld"`
	ThinHTML    bool      `json:"thin_html"` // diduga SPA/JS-rendered
	Zones       Zones     `json:"-"`         // teks per zona untuk keyword gap
	Err         string    `json:"error,omitempty"`
}

// IndexInfo menyimpan hasil URL inspection dari Google Search Console.
type IndexInfo struct {
	InspectedAt   time.Time   `json:"inspected_at"`
	Status        IndexStatus `json:"status"`
	Verdict       string      `json:"verdict"` // PASS / PARTIAL / FAIL / NEUTRAL
	RawCoverage   string      `json:"raw_coverage"`
	LastCrawlTime *time.Time  `json:"last_crawl_time,omitempty"`
	CrawledAs     string      `json:"crawled_as"` // DESKTOP / MOBILE
	PageFetch     string      `json:"page_fetch_state"`
	RobotsTxt     string      `json:"robots_txt_state"`
	IndexingState string      `json:"indexing_state"`
	GoogleCanon   string      `json:"google_canonical"`
	UserCanon     string      `json:"user_canonical"`
}

// QueryPerf menyimpan metrik satu query di Google Search.
type QueryPerf struct {
	Query       string  `json:"query"`
	Clicks      float64 `json:"clicks"`
	Impressions float64 `json:"impressions"`
	CTR         float64 `json:"ctr"`
	Position    float64 `json:"position"`
}

// PagePerf menyimpan agregasi performa pencarian halaman dari GSC.
type PagePerf struct {
	Clicks      float64     `json:"clicks"`
	Impressions float64     `json:"impressions"`
	CTR         float64     `json:"ctr"`
	Position    float64     `json:"position"` // rata-rata berbobot
	Page1       bool        `json:"page1"`    // position <= 10
	Queries     []QueryPerf `json:"queries"`
}

// QueryGap menyimpan analisis kata kunci Google vs teks halaman.
type QueryGap struct {
	QueryPerf
	Match      MatchLevel `json:"match"`
	FoundIn    []string   `json:"found_in"` // title, h1, h2h3, meta, body, url, alt
	MissingTok []string   `json:"missing_tokens"`
	Score      float64    `json:"opportunity_score"`
}

// Page adalah data gabungan lengkap untuk satu URL.
type Page struct {
	URL        string       `json:"url"`
	Path       string       `json:"path"`
	InSitemap  bool         `json:"in_sitemap"`
	InternalIn int          `json:"internal_links_in"` // jumlah link internal yang menunjuk ke sini
	Crawl      *CrawlInfo   `json:"crawl,omitempty"`
	Index      *IndexInfo   `json:"index,omitempty"`
	Perf       *PagePerf    `json:"perf,omitempty"`
	Gaps       []QueryGap   `json:"gaps,omitempty"`
	Audit      []AuditIssue `json:"audit,omitempty"`
}
