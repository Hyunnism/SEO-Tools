package indexing

import (
	"time"
)

const (
	TypeURLUpdated = "URL_UPDATED"
	TypeURLDeleted = "URL_DELETED"

	DefaultIndexingEndpoint = "https://indexing.googleapis.com/v3/urlNotifications:publish"
	DefaultMetadataEndpoint = "https://indexing.googleapis.com/v3/urlNotifications/metadata"

	PolicyWarning = `[POLICY] [GOOGLE_INDEXING_API]
Pemberitahuan Kebijakan Resmi Google:
Google secara resmi menyatakan Indexing API hanya ditujukan untuk konten JobPosting dan BroadcastEvent.
Penggunaan pada jenis konten situs lain adalah fitur opt-in dengan risiko kebijakan yang ditanggung sendiri.
Tool menerapkan pengaman: cooldown 7 hari, max 3x per hash konten, daily_cap hemat kuota, dan henti total pada 403/429.`
)

// PublishRequest adalah payload request untuk urlNotifications:publish.
type PublishRequest struct {
	URL  string `json:"url"`
	Type string `json:"type"` // URL_UPDATED / URL_DELETED
}

// NotificationRecord menyimpan data historis notifikasi dari Google.
type NotificationRecord struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	NotifyTime string `json:"notifyTime"`
}

// UrlNotificationMetadata menyimpan metadata respon notifikasi dari Google.
type UrlNotificationMetadata struct {
	URL          string              `json:"url"`
	LatestUpdate *NotificationRecord `json:"latestUpdate,omitempty"`
	LatestRemove *NotificationRecord `json:"latestRemove,omitempty"`
}

// PublishResponse adalah respon dari endpoint urlNotifications:publish.
type PublishResponse struct {
	UrlNotificationMetadata UrlNotificationMetadata `json:"urlNotificationMetadata"`
}

// IndexingRecord menyimpan riwayat pengiriman satu URL ke Google Indexing API.
type IndexingRecord struct {
	URL         string    `json:"url"`
	Type        string    `json:"type"` // URL_UPDATED / URL_DELETED
	ContentHash string    `json:"content_hash"`
	SentAt      time.Time `json:"sent_at"`
	HTTPStatus  int       `json:"http_status"`
	SendCount   int       `json:"send_count"`
}

// SiteIndexingStore menyimpan riwayat persisten pengiriman untuk satu site pada data/<site>/indexing.json.
type SiteIndexingStore struct {
	Schema    int                       `json:"schema"`
	Site      string                    `json:"site"`
	UpdatedAt time.Time                 `json:"updated_at"`
	Records   map[string]IndexingRecord `json:"records"` // Key: URL
}

// PrioritizedIndexingItem menyimpan satu item URL yang dijadwalkan untuk dikirim.
type PrioritizedIndexingItem struct {
	URL         string
	Type        string // URL_UPDATED / URL_DELETED
	Priority    int    // 1 (Tertinggi) s.d. 6
	Reason      string
	ContentHash string
}
