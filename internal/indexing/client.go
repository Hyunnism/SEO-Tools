package indexing

import (
	"context"
	"fmt"
	"net/url"

	"seo-boost/internal/google"
)

// PublishURL mengirimkan notifikasi URL_UPDATED atau URL_DELETED ke Google Indexing API.
func PublishURL(ctx context.Context, client *google.Client, targetURL, notifType string) (*PublishResponse, error) {
	if client == nil {
		return nil, fmt.Errorf("google client tidak boleh nil")
	}

	reqPayload := PublishRequest{
		URL:  targetURL,
		Type: notifType,
	}

	var resp PublishResponse
	if err := client.DoJSON(ctx, "POST", DefaultIndexingEndpoint, google.ScopeIndexing, reqPayload, &resp); err != nil {
		return nil, fmt.Errorf("gagal publish URL %s: %w", targetURL, err)
	}

	return &resp, nil
}

// GetMetadata mengambil metadata notifikasi terakhir untuk satu URL dari Google Indexing API.
func GetMetadata(ctx context.Context, client *google.Client, targetURL string) (*UrlNotificationMetadata, error) {
	if client == nil {
		return nil, fmt.Errorf("google client tidak boleh nil")
	}

	endpoint := fmt.Sprintf("%s?url=%s", DefaultMetadataEndpoint, url.QueryEscape(targetURL))

	var meta UrlNotificationMetadata
	if err := client.DoJSON(ctx, "GET", endpoint, google.ScopeIndexing, nil, &meta); err != nil {
		return nil, fmt.Errorf("gagal mengambil metadata Indexing API untuk %s: %w", targetURL, err)
	}

	return &meta, nil
}

