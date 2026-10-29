package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/model"
	"seo-boost/internal/notify"
	"seo-boost/internal/report"
)

func TestSendTelegram(t *testing.T) {
	var receivedChatID, receivedText string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedChatID = body["chat_id"]
		receivedText = body["text"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer ts.Close()

	// Override transport/client untuk mengarah ke httptest.Server
	client := ts.Client()
	client.Transport = &rewriteTransport{
		targetHost: ts.URL,
		original:   ts.Client().Transport,
	}

	err := notify.SendTelegram(context.Background(), client, "dummy-bot-token-123", "987654", "Test pesan Telegram [OK]")
	if err != nil {
		t.Fatalf("SendTelegram error: %v", err)
	}
	if receivedChatID != "987654" {
		t.Errorf("expected chat_id 987654, got %s", receivedChatID)
	}
	if !strings.Contains(receivedText, "Test pesan Telegram [OK]") {
		t.Errorf("expected received text to contain test message, got: %s", receivedText)
	}
}

func TestSendDiscord(t *testing.T) {
	var receivedContent string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedContent = body["content"]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	err := notify.SendDiscord(context.Background(), ts.Client(), ts.URL, "Test pesan Discord [INFO]")
	if err != nil {
		t.Fatalf("SendDiscord error: %v", err)
	}
	if receivedContent != "Test pesan Discord [INFO]" {
		t.Errorf("expected received content, got %s", receivedContent)
	}
}

func TestFormatNotificationText_MaxLimitAndTruncation(t *testing.T) {
	// Buat data dengan banyak URL baru untuk mengetes pemotongan 1500 karakter
	var bigList []string
	for i := 0; i < 100; i++ {
		bigList = append(bigList, "https://example.com/very/long/path/for/seo/testing/article-number-"+strings.Repeat("a", 20)+"-"+string(rune('0'+i%10)))
	}

	data := &notify.NotificationData{
		Site:      "example-site",
		Timestamp: time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC),
		Summary: &model.RunSummary{
			TotalPages:       150,
			IndexedCount:     120,
			TroubledCount:    30,
			TotalClicks:      5000,
			TotalImpressions: 120000,
			AveragePosition:  5.4,
		},
		Diff: &report.RunDiff{
			HasPrevRun: true,
			NewIndexed: bigList,
		},
		HasCriticalIssues: true,
	}

	formatted := notify.FormatNotificationText(data)
	if len(formatted) > notify.MaxNotificationLength {
		t.Errorf("panjang pesan (%d) melebihi batas maksimum (%d)", len(formatted), notify.MaxNotificationLength)
	}
	if !strings.HasSuffix(formatted, "... [pesan terpotong]") {
		t.Errorf("pesan panjang harus memiliki akhiran penanda potongan, dapat: %s", formatted[len(formatted)-30:])
	}
	if !strings.Contains(formatted, "[SEO-BOOST]") || !strings.Contains(formatted, "[WARN]") {
		t.Errorf("pesan tidak memuat header atau badge yang diharapkan")
	}
}

func TestSecretLeakPrevention(t *testing.T) {
	const secretToken = "super-secret-telegram-token-xyz"
	const secretWebhook = "https://discord.com/api/webhooks/12345/super-secret-token"

	// Request ke server yang sengaja mati/tidak ada
	badClient := &http.Client{Timeout: 50 * time.Millisecond}

	tErr := notify.SendTelegram(context.Background(), badClient, secretToken, "123", "halo")
	if tErr != nil && strings.Contains(tErr.Error(), secretToken) {
		t.Errorf("SECURITY: telegram bot token bocor ke dalam pesan error: %v", tErr)
	}

	dErr := notify.SendDiscord(context.Background(), badClient, secretWebhook, "halo")
	if dErr != nil && strings.Contains(dErr.Error(), secretWebhook) {
		t.Errorf("SECURITY: discord webhook URL bocor ke dalam pesan error: %v", dErr)
	}
}

func TestDispatch_NoiseReductionAndModes(t *testing.T) {
	var sentCount int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	os.Setenv("TEST_TELEGRAM_TOKEN", "dummy-tok")
	os.Setenv("TEST_DISCORD_WEBHOOK", ts.URL)
	defer os.Unsetenv("TEST_TELEGRAM_TOKEN")
	defer os.Unsetenv("TEST_DISCORD_WEBHOOK")

	cfg := config.Notify{
		DiscordWebhookEnv: "TEST_DISCORD_WEBHOOK",
	}

	// 1. Tidak ada perubahan bermakna, mode auto -> TIDAK dikirim
	dataNoChange := &notify.NotificationData{
		Site:      "test-site",
		Timestamp: time.Now(),
		Summary:   &model.RunSummary{TotalPages: 10},
		Diff:      &report.RunDiff{HasPrevRun: true},
	}
	sent, err := notify.Dispatch(context.Background(), ts.Client(), cfg, notify.NotifyAuto, dataNoChange)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent || sentCount != 0 {
		t.Errorf("expected no notification sent for noise reduction, sent=%v sentCount=%d", sent, sentCount)
	}

	// 2. Mode always -> HARUS dikirim meski tidak ada perubahan
	sentAlways, err := notify.Dispatch(context.Background(), ts.Client(), cfg, notify.NotifyAlways, dataNoChange)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sentAlways || sentCount != 1 {
		t.Errorf("expected notification sent for mode always, sent=%v sentCount=%d", sentAlways, sentCount)
	}

	// 3. Mode off -> TIDAK dikirim meski ada perubahan bermakna
	dataMeaningful := &notify.NotificationData{
		Site:              "test-site",
		Timestamp:         time.Now(),
		Summary:           &model.RunSummary{TotalPages: 10},
		Diff:              &report.RunDiff{HasPrevRun: true, NewIndexed: []string{"/new"}},
		HasCriticalIssues: true,
	}
	sentOff, _ := notify.Dispatch(context.Background(), ts.Client(), cfg, notify.NotifyOff, dataMeaningful)
	if sentOff || sentCount != 1 {
		t.Errorf("expected no notification sent for mode off")
	}

	// 4. Ada perubahan bermakna, mode auto -> HARUS dikirim
	sentAuto, err := notify.Dispatch(context.Background(), ts.Client(), cfg, notify.NotifyAuto, dataMeaningful)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sentAuto || sentCount != 2 {
		t.Errorf("expected notification sent for meaningful change, sent=%v sentCount=%d", sentAuto, sentCount)
	}
}

type rewriteTransport struct {
	targetHost string
	original   http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Arahkan request Telegram bot ke URL httptest targetHost
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = strings.TrimPrefix(t.targetHost, "http://")
	return http.DefaultTransport.RoundTrip(newReq)
}
