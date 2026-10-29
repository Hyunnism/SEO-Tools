package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultEndpoint = "https://api.indexnow.org/indexnow"
	MaxBatchSize    = 10000
)

// SubmitRequest adalah payload JSON yang dikirimkan ke endpoint IndexNow.
type SubmitRequest struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation,omitempty"`
	URLList     []string `json:"urlList"`
}

// SubmitResult menyimpan hasil eksekusi pengiriman ke IndexNow.
type SubmitResult struct {
	TotalSubmitted int    `json:"total_submitted"`
	Batches        int    `json:"batches"`
	StatusCode     int    `json:"status_code"`
	Message        string `json:"message"`
}

// VerifyKeyFile memeriksa apakah file kunci di server target dapat diakses dan isinya cocok dengan kunci yang diharapkan.
func VerifyKeyFile(ctx context.Context, hc *http.Client, keyLocation, expectedKey string) error {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, keyLocation, nil)
	if err != nil {
		return fmt.Errorf("URL keyLocation tidak valid %q: %w", keyLocation, err)
	}
	req.Header.Set("User-Agent", "seo-boost/1.0 (key-verifier)")

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("gagal menghubungi keyLocation %s: %w", keyLocation, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("file kunci tidak dapat diakses di %s (HTTP %d). Pastikan file telah diunggah ke hosting", keyLocation, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return fmt.Errorf("gagal membaca isi file kunci: %w", err)
	}

	content := strings.TrimSpace(string(bodyBytes))
	if content != expectedKey {
		return fmt.Errorf("isi file kunci di %s tidak cocok. Diharapkan: %q, ditemukan: %q", keyLocation, expectedKey, content)
	}

	return nil
}

// Submitter menyediakan antarmuka pengiriman URL ke IndexNow dengan opsi endpoint kustom (untuk testing).
type Submitter struct {
	Endpoint string
	Client   *http.Client
}

// NewSubmitter membuat instance Submitter baru.
func NewSubmitter(endpoint string, hc *http.Client) *Submitter {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Submitter{
		Endpoint: endpoint,
		Client:   hc,
	}
}

// Submit mengirimkan daftar URL ke IndexNow, memecah dalam batch jika lebih dari 10.000 URL.
func (s *Submitter) Submit(ctx context.Context, host, key, keyLocation string, urlList []string) (*SubmitResult, error) {
	if err := ValidateKeyFormat(key); err != nil {
		return nil, fmt.Errorf("kunci IndexNow tidak valid: %w", err)
	}

	if len(urlList) == 0 {
		return &SubmitResult{
			TotalSubmitted: 0,
			Batches:        0,
			StatusCode:     http.StatusOK,
			Message:        "tidak ada URL yang perlu dikirim",
		}, nil
	}

	total := len(urlList)
	batches := 0
	lastStatus := http.StatusOK

	for i := 0; i < total; i += MaxBatchSize {
		end := i + MaxBatchSize
		if end > total {
			end = total
		}
		chunk := urlList[i:end]
		batches++

		payload := SubmitRequest{
			Host:        host,
			Key:         key,
			KeyLocation: keyLocation,
			URLList:     chunk,
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("gagal marshal payload IndexNow: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(payloadBytes))
		if err != nil {
			return nil, fmt.Errorf("gagal membuat request IndexNow: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("User-Agent", "seo-boost/1.0 (indexnow)")

		resp, err := s.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request ke endpoint IndexNow gagal pada batch ke-%d: %w", batches, err)
		}
		defer resp.Body.Close()

		lastStatus = resp.StatusCode
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		respMsg := strings.TrimSpace(string(respBody))

		switch resp.StatusCode {
		case http.StatusOK, http.StatusAccepted:
			// Sukses
		case http.StatusBadRequest:
			return nil, fmt.Errorf("IndexNow HTTP 400 (Bad Request): format JSON atau URL tidak valid: %s", respMsg)
		case http.StatusForbidden:
			return nil, fmt.Errorf("IndexNow HTTP 403 (Forbidden): kunci tidak valid atau keyLocation tidak ditemukan: %s", respMsg)
		case http.StatusUnprocessableEntity:
			return nil, fmt.Errorf("IndexNow HTTP 422 (Unprocessable Entity): URL tidak cocok dengan domain host %q atau kunci salah: %s", host, respMsg)
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("IndexNow HTTP 429 (Too Many Requests): kuota request terlampaui: %s", respMsg)
		default:
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("IndexNow error HTTP %d: %s", resp.StatusCode, respMsg)
			}
		}
	}

	msg := "URL berhasil diterima oleh IndexNow"
	if lastStatus == http.StatusAccepted {
		msg = "URL diterima oleh IndexNow (validasi kunci sedang diproses)"
	}

	return &SubmitResult{
		TotalSubmitted: total,
		Batches:        batches,
		StatusCode:     lastStatus,
		Message:        msg,
	}, nil
}

