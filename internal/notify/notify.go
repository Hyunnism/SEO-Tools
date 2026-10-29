package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"seo-boost/internal/config"
	"seo-boost/internal/model"
	"seo-boost/internal/report"
)

const MaxNotificationLength = 1500

type NotifyMode string

const (
	NotifyAuto   NotifyMode = "auto"
	NotifyAlways NotifyMode = "always"
	NotifyOff    NotifyMode = "off"
)

// NotificationData membungkus ringkasan dan diff eksekusi pipeline untuk diformat ke notifikasi.
type NotificationData struct {
	Site              string
	Timestamp         time.Time
	Summary           *model.RunSummary
	Diff              *report.RunDiff
	HasCriticalIssues bool
}

// FormatNotificationText memformat pesan ringkas berorientasi teks ASCII padat (< 1500 karakter).
func FormatNotificationText(data *NotificationData) string {
	if data == nil {
		return ""
	}

	var sb strings.Builder
	ts := data.Timestamp.UTC().Format("2006-01-02 15:04 UTC")
	sb.WriteString(fmt.Sprintf("[SEO-BOOST] Laporan: %s (%s)\n", data.Site, ts))
	if data.HasCriticalIssues {
		sb.WriteString("Status   : [WARN] Ditemukan masalah kritis teknis SEO.\n")
	} else {
		sb.WriteString("Status   : [OK] Pipeline selesai bersih tanpa masalah kritis.\n")
	}
	sb.WriteString("--------------------------------------------------\n")

	if data.Summary != nil {
		sb.WriteString(fmt.Sprintf("Ringkasan: %d URL | %d Terindeks | %d Bermasalah\n",
			data.Summary.TotalPages, data.Summary.IndexedCount, data.Summary.TroubledCount))
		sb.WriteString(fmt.Sprintf("Performa : %.0f klik | %.0f imp | Pos rata-rata %.1f\n",
			data.Summary.TotalClicks, data.Summary.TotalImpressions, data.Summary.AveragePosition))
	}

	if data.Diff != nil && data.Diff.HasPrevRun {
		sb.WriteString("--------------------------------------------------\n")
		sb.WriteString("Diff & Perubahan:\n")

		if len(data.Diff.NewIndexed) > 0 {
			sb.WriteString(fmt.Sprintf("[+] %d baru terindeks:\n", len(data.Diff.NewIndexed)))
			for _, u := range data.Diff.NewIndexed {
				sb.WriteString(fmt.Sprintf("    [+] %s\n", u))
			}
		}
		if len(data.Diff.DeIndexed) > 0 {
			sb.WriteString(fmt.Sprintf("[-] %d hilang dari indeks:\n", len(data.Diff.DeIndexed)))
			for _, u := range data.Diff.DeIndexed {
				sb.WriteString(fmt.Sprintf("    [-] %s\n", u))
			}
		}
		if len(data.Diff.RankGains) > 0 {
			sb.WriteString(fmt.Sprintf("[UP] %d naik peringkat >= 3:\n", len(data.Diff.RankGains)))
			for _, g := range data.Diff.RankGains {
				sb.WriteString(fmt.Sprintf("    [UP] %s (%.1f -> %.1f)\n", g.URL, g.OldPosition, g.NewPosition))
			}
		}
		if len(data.Diff.RankDrops) > 0 {
			sb.WriteString(fmt.Sprintf("[DOWN] %d turun peringkat >= 3:\n", len(data.Diff.RankDrops)))
			for _, d := range data.Diff.RankDrops {
				sb.WriteString(fmt.Sprintf("    [DOWN] %s (%.1f -> %.1f)\n", d.URL, d.OldPosition, d.NewPosition))
			}
		}
		if !data.Diff.HasMeaningfulChanges() {
			sb.WriteString("[INFO] Tidak ada perubahan status atau pergeseran ranking signifikan.\n")
		}
	}

	text := sb.String()
	if len(text) > MaxNotificationLength {
		const suffix = "\n... [pesan terpotong]"
		cutoff := MaxNotificationLength - len(suffix)
		if cutoff < 0 {
			cutoff = 0
		}
		text = text[:cutoff] + suffix
	}
	return text
}

// SendTelegram mengirimkan pesan ke Telegram Bot API menggunakan bot token dan chat ID.
func SendTelegram(ctx context.Context, hc *http.Client, token string, chatID string, text string) error {
	if token == "" {
		return errors.New("telegram bot token kosong")
	}
	if chatID == "" {
		return errors.New("telegram chat_id kosong")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    text,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("gagal marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("gagal membuat request telegram: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		// ponytail: jangan sertakan URL lengkap agar token di path bot<token> tidak bocor ke log
		return fmt.Errorf("gagal kirim notifikasi telegram: koneksi error")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api mengembalikan status HTTP %d", resp.StatusCode)
	}
	return nil
}

// SendDiscord mengirimkan pesan ringkas ke Discord webhook URL.
func SendDiscord(ctx context.Context, hc *http.Client, webhookURL string, text string) error {
	if webhookURL == "" {
		return errors.New("discord webhook URL kosong")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}

	payload := map[string]string{
		"content": text,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("gagal marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("gagal membuat request discord: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		// Masking URL webhook agar token rahasia webhook tidak bocor
		return fmt.Errorf("gagal kirim notifikasi discord: koneksi error")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook mengembalikan status HTTP %d", resp.StatusCode)
	}
	return nil
}

var (
	ErrNoChannelsConfigured = errors.New("tidak ada kanal notifikasi yang aktif (periksa environment variable Telegram/Discord)")
)

// Dispatch mengevaluasi kondisi kirim (noise reduction) dan mengirimkan notifikasi ke kanal yang aktif.
func Dispatch(ctx context.Context, hc *http.Client, cfg config.Notify, mode NotifyMode, data *NotificationData) (sent bool, err error) {
	if mode == NotifyOff {
		return false, nil
	}

	// Evaluasi perubahan bermakna
	meaningful := data.HasCriticalIssues || (data.Diff != nil && data.Diff.HasMeaningfulChanges())
	if !meaningful && mode != NotifyAlways {
		return false, nil
	}

	// Ambil token dan webhook HANYA dari environment variables
	var telegramToken string
	if cfg.TelegramTokenEnv != "" {
		telegramToken = strings.TrimSpace(os.Getenv(cfg.TelegramTokenEnv))
	}
	telegramChatID := strings.TrimSpace(cfg.TelegramChatID)

	var discordWebhook string
	if cfg.DiscordWebhookEnv != "" {
		discordWebhook = strings.TrimSpace(os.Getenv(cfg.DiscordWebhookEnv))
	}

	// Jika tidak ada konfigurasi kanal yang siap, beritahukan pemanggil
	if (telegramToken == "" || telegramChatID == "") && discordWebhook == "" {
		return false, ErrNoChannelsConfigured
	}

	text := FormatNotificationText(data)
	var errs []error

	if telegramToken != "" && telegramChatID != "" {
		if tErr := SendTelegram(ctx, hc, telegramToken, telegramChatID, text); tErr != nil {
			errs = append(errs, tErr)
		} else {
			sent = true
		}
	}

	if discordWebhook != "" {
		if dErr := SendDiscord(ctx, hc, discordWebhook, text); dErr != nil {
			errs = append(errs, dErr)
		} else {
			sent = true
		}
	}

	return sent, errors.Join(errs...)
}

// ReadMockBody helper untuk membaca isi body tanpa membocorkan kredensial.
func ReadMockBody(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
