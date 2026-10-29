package indexnow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateKeyAndValidation(t *testing.T) {
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	if len(k1) != 32 {
		t.Errorf("expected key length 32, got %d (%s)", len(k1), k1)
	}
	if err := ValidateKeyFormat(k1); err != nil {
		t.Errorf("expected generated key to be valid, got: %v", err)
	}

	k2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey second call error: %v", err)
	}
	if k1 == k2 {
		t.Errorf("generated keys should be uniquely random")
	}

	// Kasus tidak valid
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"terlalu pendek", "abc", true},
		{"terlalu panjang", strings.Repeat("a", 129), true},
		{"bukan hex", "12345678xyz", true},
		{"valid batas bawah 8", "12345678", false},
		{"valid batas atas 128", strings.Repeat("f", 128), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateKeyFormat(c.key)
			if (err != nil) != c.wantErr {
				t.Errorf("key %q: got error %v, wantErr %v", c.key, err, c.wantErr)
			}
		})
	}
}

func TestVerifyKeyFile(t *testing.T) {
	expectedKey := "a1b2c3d4e5f60718293a4b5c6d7e8f90"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid.txt":
			fmt.Fprintln(w, expectedKey)
		case "/wrong.txt":
			fmt.Fprintln(w, "something-else")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	// 1. Sukses
	err := VerifyKeyFile(ctx, server.Client(), server.URL+"/valid.txt", expectedKey)
	if err != nil {
		t.Errorf("expected verification success, got: %v", err)
	}

	// 2. Gagal: isi berbeda
	err = VerifyKeyFile(ctx, server.Client(), server.URL+"/wrong.txt", expectedKey)
	if err == nil || !strings.Contains(err.Error(), "tidak cocok") {
		t.Errorf("expected content mismatch error, got: %v", err)
	}

	// 3. Gagal: 404
	err = VerifyKeyFile(ctx, server.Client(), server.URL+"/not-found.txt", expectedKey)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

func TestSubmit(t *testing.T) {
	key := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	host := "example.com"
	keyLocation := "https://example.com/a1b2c3d4e5f60718293a4b5c6d7e8f90.txt"

	var receivedBatches []SubmitRequest
	statusToReturn := http.StatusOK

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload SubmitRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Bad JSON", http.StatusBadRequest)
			return
		}
		receivedBatches = append(receivedBatches, payload)

		w.WriteHeader(statusToReturn)
		if statusToReturn >= 400 {
			fmt.Fprintf(w, "Error status %d", statusToReturn)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	submitter := NewSubmitter(server.URL, server.Client())

	// 1. Submit normal (2 URL, status 200)
	urls := []string{"https://example.com/page1", "https://example.com/page2"}
	res, err := submitter.Submit(ctx, host, key, keyLocation, urls)
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}
	if res.TotalSubmitted != 2 || res.Batches != 1 || res.StatusCode != http.StatusOK {
		t.Errorf("unexpected result: %+v", res)
	}
	if len(receivedBatches) != 1 || len(receivedBatches[0].URLList) != 2 {
		t.Errorf("unexpected received batches: %+v", receivedBatches)
	}

	// 2. Status 202 Accepted
	receivedBatches = nil
	statusToReturn = http.StatusAccepted
	res, err = submitter.Submit(ctx, host, key, keyLocation, urls)
	if err != nil {
		t.Fatalf("Submit 202 error: %v", err)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", res.StatusCode)
	}

	// 3. Status 403 Forbidden
	statusToReturn = http.StatusForbidden
	_, err = submitter.Submit(ctx, host, key, keyLocation, urls)
	if err == nil || !strings.Contains(err.Error(), "403 (Forbidden)") {
		t.Errorf("expected 403 error, got: %v", err)
	}

	// 4. Status 422 Unprocessable Entity
	statusToReturn = http.StatusUnprocessableEntity
	_, err = submitter.Submit(ctx, host, key, keyLocation, urls)
	if err == nil || !strings.Contains(err.Error(), "422 (Unprocessable Entity)") {
		t.Errorf("expected 422 error, got: %v", err)
	}

	// 5. Chunking > 10.000 URL
	receivedBatches = nil
	statusToReturn = http.StatusOK
	largeURLList := make([]string, 20005)
	for i := range largeURLList {
		largeURLList[i] = fmt.Sprintf("https://example.com/item/%d", i)
	}

	res, err = submitter.Submit(ctx, host, key, keyLocation, largeURLList)
	if err != nil {
		t.Fatalf("Submit large error: %v", err)
	}
	if res.Batches != 3 || res.TotalSubmitted != 20005 {
		t.Errorf("expected 3 batches for 20005 URLs, got: %+v", res)
	}
	if len(receivedBatches) != 3 {
		t.Fatalf("expected 3 requests to server, got %d", len(receivedBatches))
	}
	if len(receivedBatches[0].URLList) != 10000 || len(receivedBatches[1].URLList) != 10000 || len(receivedBatches[2].URLList) != 5 {
		t.Errorf("chunk sizes incorrect: %d, %d, %d",
			len(receivedBatches[0].URLList), len(receivedBatches[1].URLList), len(receivedBatches[2].URLList))
	}
}

