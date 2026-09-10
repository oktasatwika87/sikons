// Package notifier menangani pengiriman email notifikasi.
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Sender adalah antarmuka untuk pengiriman email.
// Bisa diganti dengan mock saat testing.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// ResendSender mengirim email via Resend API.
type ResendSender struct {
	APIKey     string
	From       string
	BaseURL    string // default "https://api.resend.com" kalau kosong
	HTTPClient *http.Client
}

// DefaultBaseURL adalah endpoint API Resend.
const DefaultBaseURL = "https://api.resend.com"

// Send mengirim email via Resend.
// Kirim POST ke {BaseURL}/emails dengan header Authorization: Bearer {APIKey}
// dan body JSON {"from": r.From, "to": [to], "subject": subject, "html": body}.
func (r *ResendSender) Send(ctx context.Context, to, subject, body string) error {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	payload := map[string]any{
		"from":    r.From,
		"to":      []string{to},
		"subject": subject,
		"html":    body,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/emails", bytes.NewReader(jsonPayload))
	if err != nil {
		return fmt.Errorf("buat request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("kirim request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Baca body response untuk debugging.
		// Body mungkin besar, batasi bacaan.
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		respBody := string(buf[:n])
		return fmt.Errorf("resend API error: status %d, body: %s", resp.StatusCode, respBody)
	}

	return nil
}
