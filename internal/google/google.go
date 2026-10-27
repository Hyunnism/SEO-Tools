package google

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ScopeWebmastersReadonly = "https://www.googleapis.com/auth/webmasters.readonly"
	ScopeWebmasters         = "https://www.googleapis.com/auth/webmasters"
	ScopeIndexing           = "https://www.googleapis.com/auth/indexing"

	DefaultTokenURI = "https://oauth2.googleapis.com/token"
)

type ServiceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type cachedToken struct {
	val string
	exp time.Time
}

type Client struct {
	sa       ServiceAccountKey
	parsedPK *rsa.PrivateKey
	http     *http.Client
	logger   *slog.Logger

	mu    sync.Mutex
	cache map[string]cachedToken

	// Untuk testing: bisa dioverride agar retry tidak membuang waktu
	baseRetryDelay time.Duration
	maxRetries     int
}

type ClientOption func(*Client)

func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.http = hc
	}
}

func WithLogger(l *slog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = l
	}
}

func WithRetrySettings(baseDelay time.Duration, maxRetries int) ClientOption {
	return func(c *Client) {
		c.baseRetryDelay = baseDelay
		c.maxRetries = maxRetries
	}
}

// NewClientFromFile membaca file JSON Service Account dan mengembalikan Client.
func NewClientFromFile(path string, opts ...ClientOption) (*Client, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca service account key: %w", err)
	}
	return NewClientFromBytes(b, opts...)
}

// NewClientFromBytes membuat Client dari konten JSON Service Account.
func NewClientFromBytes(b []byte, opts ...ClientOption) (*Client, error) {
	var sa ServiceAccountKey
	if err := json.Unmarshal(b, &sa); err != nil {
		return nil, fmt.Errorf("service account JSON tidak valid: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = DefaultTokenURI
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("service account key harus memiliki client_email dan private_key")
	}

	pk, err := parsePrivateKey(sa.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("private key tidak valid: %w", err)
	}

	c := &Client{
		sa:             sa,
		parsedPK:       pk,
		http:           &http.Client{Timeout: 30 * time.Second},
		logger:         slog.Default(),
		cache:          make(map[string]cachedToken),
		baseRetryDelay: 500 * time.Millisecond,
		maxRetries:     4,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

func parsePrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("private_key bukan format PEM valid")
	}

	// 1. Coba PKCS#8 (format standar Google Service Account)
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key bukan bertipe RSA")
		}
		return rsaKey, nil
	}

	// 2. Fallback coba PKCS#1
	rsaKey, err1 := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err1 == nil {
		return rsaKey, nil
	}

	return nil, fmt.Errorf("gagal mengurai private key (PKCS#8: %v; PKCS#1: %v)", err, err1)
}

var b64 = base64.RawURLEncoding

// Token mengembalikan bearer access token untuk scope yang diminta.
// Menggunakan cache dan auto-refresh jika tersisa kurang dari 60 detik.
func (c *Client) Token(ctx context.Context, scope string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if t, ok := c.cache[scope]; ok && t.exp.Sub(now) > time.Minute {
		return t.val, nil
	}

	token, exp, err := c.fetchToken(ctx, scope)
	if err != nil {
		return "", err
	}

	c.cache[scope] = cachedToken{
		val: token,
		exp: exp,
	}
	return token, nil
}

func (c *Client) fetchToken(ctx context.Context, scope string) (string, time.Time, error) {
	now := time.Now()
	claims, err := json.Marshal(map[string]any{
		"iss":   c.sa.ClientEmail,
		"scope": scope,
		"aud":   c.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}

	unsigned := b64.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.parsedPK, crypto.SHA256, sum[:])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gagal menandatangani JWT: %w", err)
	}

	assertion := unsigned + "." + b64.EncodeToString(sig)
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	c.logDebug("meminta access token Google", "scope", scope)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("permintaan token gagal: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("gagal membaca respons token: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &errResp)
		desc := errResp.Description
		if desc == "" {
			desc = errResp.Error
		}
		if desc == "" {
			desc = string(body)
		}
		return "", time.Time{}, fmt.Errorf("token request ditolak Google (HTTP %d): %s", resp.StatusCode, desc)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, fmt.Errorf("gagal decode JSON respons token: %w", err)
	}
	if out.AccessToken == "" {
		return "", time.Time{}, errors.New("Google mengembalikan access_token kosong")
	}

	expiresAt := now.Add(time.Duration(out.ExpiresIn) * time.Second)
	return out.AccessToken, expiresAt, nil
}

// DoJSON mengirim HTTP request dengan Authorization Bearer token dan melakukan retry pada status 429 atau 5xx.
// Header Authorization disanitasi dan tidak pernah dilog.
func (c *Client) DoJSON(ctx context.Context, method, endpoint, scope string, reqBody any, respData any) error {
	var bodyBytes []byte
	if reqBody != nil {
		var err error
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("gagal marshal request JSON: %w", err)
		}
	}

	token, err := c.Token(ctx, scope)
	if err != nil {
		return fmt.Errorf("autentikasi gagal: %w", err)
	}

	attempt := 0
	for {
		attempt++
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		c.logDebug("mengirim request HTTP", "method", method, "url", endpoint, "attempt", attempt)

		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt <= c.maxRetries {
				delay := c.calculateBackoff(attempt, nil)
				c.logDebug("request error jaringan, retry", "err", err, "delay", delay)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					continue
				}
			}
			return fmt.Errorf("HTTP request error setelah %d percobaan: %w", attempt, err)
		}

		// Jika status 429 atau 5xx, coba retry
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt <= c.maxRetries {
			retryAfterHeader := resp.Header.Get("Retry-After")
			resp.Body.Close()

			delay := c.calculateBackoff(attempt, &retryAfterHeader)
			c.logDebug("menerima status retryable dari Google", "status", resp.StatusCode, "delay", delay)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("gagal membaca response body: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("API request gagal (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
		}

		if respData != nil && len(respBytes) > 0 {
			if err := json.Unmarshal(respBytes, respData); err != nil {
				return fmt.Errorf("gagal unmarshal respons JSON: %w", err)
			}
		}
		return nil
	}
}

func (c *Client) calculateBackoff(attempt int, retryAfterHeader *string) time.Duration {
	if retryAfterHeader != nil && *retryAfterHeader != "" {
		if seconds, err := strconv.Atoi(*retryAfterHeader); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		if t, err := http.ParseTime(*retryAfterHeader); err == nil {
			if wait := time.Until(t); wait > 0 {
				return wait
			}
		}
	}

	// Exponential backoff: baseDelay * 2^(attempt-1) + jitter
	multiplier := 1 << (attempt - 1)
	base := c.baseRetryDelay * time.Duration(multiplier)
	if base > 30*time.Second {
		base = 30 * time.Second
	}

	// Jitter acak 0-250ms
	n, _ := rand.Int(rand.Reader, big.NewInt(250))
	jitter := time.Duration(n.Int64()) * time.Millisecond
	return base + jitter
}

func (c *Client) logDebug(msg string, args ...any) {
	if c.logger != nil {
		c.logger.Debug(msg, args...)
	}
}

// SiteEntry mewakili properti situs dari Google Search Console.
type SiteEntry struct {
	SiteURL         string `json:"siteUrl"`
	PermissionLevel string `json:"permissionLevel"`
}

type sitesListResponse struct {
	SiteEntry []SiteEntry `json:"siteEntry"`
}

// ListSites mengambil daftar seluruh properti situs yang dapat diakses service account di GSC.
func (c *Client) ListSites(ctx context.Context) ([]SiteEntry, error) {
	endpoint := "https://www.googleapis.com/webmasters/v3/sites"
	var resp sitesListResponse
	if err := c.DoJSON(ctx, http.MethodGet, endpoint, ScopeWebmastersReadonly, nil, &resp); err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar properti GSC: %w", err)
	}
	return resp.SiteEntry, nil
}

type inspectURLRequest struct {
	SiteURL       string `json:"siteUrl"`
	InspectionURL string `json:"inspectionUrl"`
	LanguageCode  string `json:"languageCode"`
}

// IndexStatusResult mewakili bagian indexStatusResult dari respons GSC URL Inspection.
type IndexStatusResult struct {
	Verdict         string `json:"verdict"`
	CoverageState   string `json:"coverageState"`
	RobotsTxtState  string `json:"robotsTxtState"`
	IndexingState   string `json:"indexingState"`
	LastCrawlTime   string `json:"lastCrawlTime"`
	PageFetchState  string `json:"pageFetchState"`
	GoogleCanonical string `json:"googleCanonical"`
	UserCanonical   string `json:"userCanonical"`
	CrawledAs       string `json:"crawledAs"`
}

type inspectURLResponse struct {
	InspectionResult struct {
		InspectionURL     string             `json:"inspectionUrl"`
		IndexStatusResult *IndexStatusResult `json:"indexStatusResult"`
	} `json:"inspectionResult"`
}

// InspectURL memanggil Google URL Inspection API untuk memeriksa status crawl dan index suatu URL.
func (c *Client) InspectURL(ctx context.Context, siteURL, inspectionURL string) (*IndexStatusResult, error) {
	endpoint := "https://searchconsole.googleapis.com/v1/urlInspection/index:inspect"
	reqBody := inspectURLRequest{
		SiteURL:       siteURL,
		InspectionURL: inspectionURL,
		LanguageCode:  "id-ID",
	}
	var resp inspectURLResponse
	if err := c.DoJSON(ctx, http.MethodPost, endpoint, ScopeWebmastersReadonly, reqBody, &resp); err != nil {
		return nil, err
	}
	if resp.InspectionResult.IndexStatusResult == nil {
		return nil, errors.New("respons GSC urlInspection tidak memuat indexStatusResult")
	}
	return resp.InspectionResult.IndexStatusResult, nil
}

// SearchAnalyticsRow mewakili satu baris data gabungan page dan query dari Search Analytics GSC.
type SearchAnalyticsRow struct {
	Keys        []string `json:"keys"`
	Clicks      float64  `json:"clicks"`
	Impressions float64  `json:"impressions"`
	CTR         float64  `json:"ctr"`
	Position    float64  `json:"position"`
}

type searchAnalyticsQueryRequest struct {
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	Dimensions []string `json:"dimensions"`
	RowLimit   int      `json:"rowLimit"`
	StartRow   int      `json:"startRow"`
	DataState  string   `json:"dataState"`
}

type searchAnalyticsResponse struct {
	Rows []SearchAnalyticsRow `json:"rows"`
}

// QuerySearchAnalytics mengambil metrik performa pencarian GSC dengan paginasi otomatis per 25.000 baris.
func (c *Client) QuerySearchAnalytics(ctx context.Context, siteURL string, startDate, endDate string, rowLimit int) ([]SearchAnalyticsRow, error) {
	endpoint := "https://www.googleapis.com/webmasters/v3/sites/" + url.PathEscape(siteURL) + "/searchAnalytics/query"
	if rowLimit <= 0 {
		rowLimit = 25000
	}

	batchSize := 25000
	if rowLimit < batchSize {
		batchSize = rowLimit
	}

	var allRows []SearchAnalyticsRow
	startRow := 0

	for {
		reqBody := searchAnalyticsQueryRequest{
			StartDate:  startDate,
			EndDate:    endDate,
			Dimensions: []string{"page", "query"},
			RowLimit:   batchSize,
			StartRow:   startRow,
			DataState:  "final",
		}

		var resp searchAnalyticsResponse
		if err := c.DoJSON(ctx, http.MethodPost, endpoint, ScopeWebmastersReadonly, reqBody, &resp); err != nil {
			return nil, err
		}

		allRows = append(allRows, resp.Rows...)

		if len(resp.Rows) < batchSize || len(allRows) >= rowLimit {
			break
		}

		startRow += len(resp.Rows)
	}

	if len(allRows) > rowLimit {
		allRows = allRows[:rowLimit]
	}

	return allRows, nil
}

// IsErrQuotaExceeded memeriksa apakah error mengindikasikan kuota habis (HTTP 429).
func IsErrQuotaExceeded(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "status 429") || strings.Contains(err.Error(), "Too Many Requests")
}

