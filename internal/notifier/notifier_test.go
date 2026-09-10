package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendSender_Send_Success(t *testing.T) {
	var receivedReq *http.Request
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReq = r.Clone(context.Background())
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := &ResendSender{
		APIKey:  "test-api-key",
		From:    "Test <test@example.com>",
		BaseURL: server.URL,
	}

	err := sender.Send(context.Background(), "student@example.com", "Test Subject", "<p>Test body</p>")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Verifikasi header Authorization.
	if got := receivedReq.Header.Get("Authorization"); got != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer test-api-key")
	}

	// Verifikasi Content-Type.
	if got := receivedReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", got, "application/json")
	}

	// Verifikasi body JSON.
	if got, ok := receivedBody["from"].(string); !ok || got != "Test <test@example.com>" {
		t.Errorf("from = %v, want %q", receivedBody["from"], "Test <test@example.com>")
	}

	toSlice, ok := receivedBody["to"].([]any)
	if !ok {
		t.Fatalf("to is not a slice")
	}
	if len(toSlice) != 1 || toSlice[0] != "student@example.com" {
		t.Errorf("to = %v, want [student@example.com]", toSlice)
	}

	if got := receivedBody["subject"]; got != "Test Subject" {
		t.Errorf("subject = %v, want %q", got, "Test Subject")
	}

	if got := receivedBody["html"]; got != "<p>Test body</p>" {
		t.Errorf("html = %v, want %q", got, "<p>Test body</p>")
	}
}

func TestResendSender_Send_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message": "Invalid API key"}`)
	}))
	defer server.Close()

	sender := &ResendSender{
		APIKey:  "bad-key",
		From:    "Test <test@example.com>",
		BaseURL: server.URL,
	}

	err := sender.Send(context.Background(), "student@example.com", "Test", "body")
	if err == nil {
		t.Fatal("Send() error = nil, want non-nil")
	}

	// Error harus menyertakan status code dan body.
	errStr := err.Error()
	if !strings.Contains(errStr, "401") {
		t.Errorf("error = %q, want to contain status code 401", errStr)
	}
	if !strings.Contains(errStr, "Invalid API key") {
		t.Errorf("error = %q, want to contain response body", errStr)
	}
}
