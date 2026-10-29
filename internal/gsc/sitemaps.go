package gsc

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"seo-boost/internal/google"
)

// SitemapContent menyimpan rincian tipe konten dan jumlah URL sitemap.
type SitemapContent struct {
	Type      string `json:"type"`
	Submitted string `json:"submitted"`
	Indexed   string `json:"indexed"`
}

// SitemapStatus menyimpan metadata status sitemap dari Google Search Console.
type SitemapStatus struct {
	Path            string           `json:"path"`
	LastSubmitted   *time.Time       `json:"lastSubmitted,omitempty"`
	IsPending       bool             `json:"isPending"`
	IsSitemapsIndex bool             `json:"isSitemapsIndex"`
	LastDownloaded  *time.Time       `json:"lastDownloaded,omitempty"`
	Warnings        string           `json:"warnings"`
	Errors          string           `json:"errors"`
	Contents        []SitemapContent `json:"contents,omitempty"`
}

type sitemapsListResponse struct {
	Sitemap []SitemapStatus `json:"sitemap"`
}

// SubmitSitemap mengirimkan sinyal sitemap ke Google Search Console (PUT /webmasters/v3/sites/{site}/sitemaps/{feedpath}).
func SubmitSitemap(ctx context.Context, client *google.Client, siteProperty, sitemapURL string) error {
	if client == nil {
		return fmt.Errorf("google client tidak boleh nil")
	}

	escapedSite := url.PathEscape(siteProperty)
	escapedFeed := url.PathEscape(sitemapURL)
	endpoint := fmt.Sprintf("https://www.googleapis.com/webmasters/v3/sites/%s/sitemaps/%s", escapedSite, escapedFeed)

	// PUT request tanpa request body dan response body
	if err := client.DoJSON(ctx, "PUT", endpoint, google.ScopeWebmasters, nil, nil); err != nil {
		return fmt.Errorf("gagal submit sitemap %s ke GSC: %w", sitemapURL, err)
	}

	return nil
}

// ListSitemaps mengambil status seluruh sitemap yang terdaftar di Google Search Console.
func ListSitemaps(ctx context.Context, client *google.Client, siteProperty string) ([]SitemapStatus, error) {
	if client == nil {
		return nil, fmt.Errorf("google client tidak boleh nil")
	}

	escapedSite := url.PathEscape(siteProperty)
	endpoint := fmt.Sprintf("https://www.googleapis.com/webmasters/v3/sites/%s/sitemaps", escapedSite)

	var resp sitemapsListResponse
	if err := client.DoJSON(ctx, "GET", endpoint, google.ScopeWebmasters, nil, &resp); err != nil {
		return nil, fmt.Errorf("gagal mengambil status sitemap dari GSC: %w", err)
	}

	return resp.Sitemap, nil
}
