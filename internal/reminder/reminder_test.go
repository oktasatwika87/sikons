package reminder

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

// TestFormatEmailBody memverifikasi format email.
func TestFormatEmailBody(t *testing.T) {
	jakarta, _ := time.LoadLocation("Asia/Jakarta")
	// 6 April 2026 itu Senin. UTC 9:00 = WIB 16:00.
	slotTime := time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)

	body := formatEmailBody("Budi", "Dr. Siti", slotTime, "Tesis", jakarta)

	if !contains(body, "Budi") {
		t.Error("body should contain student name")
	}
	if !contains(body, "Dr. Siti") {
		t.Error("body should contain lecturer name")
	}
	// Waktu di-convert ke Asia/Jakarta: 09:00 UTC = 16:00 WIB
	if !contains(body, "Senin, 6 April 2026 pukul 16.00 WIB") {
		t.Errorf("body should contain formatted time, got: %s", body)
	}
	if !contains(body, "Tesis") {
		t.Error("body should contain topic")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestTruncateError memverifikasi truncateError.
func TestTruncateError(t *testing.T) {
	shortErr := errors.New("short error")
	if got := truncateError(shortErr); got != "short error" {
		t.Errorf("truncateError(short) = %q, want %q", got, "short error")
	}

	longMsg := ""
	for i := 0; i < 600; i++ {
		longMsg += "x"
	}
	longErr := errors.New(longMsg)
	truncated := truncateError(longErr)
	if len(truncated) > 503 { // 500 + "..."
		t.Errorf("truncateError(long) length = %d, want <= 503", len(truncated))
	}
	if !hasSuffix(truncated, "...") {
		t.Errorf("truncateError(long) = %q, want to end with ...", truncated)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestNotificationStatusConstants memverifikasi bahwa status constants benar.
func TestNotificationStatusConstants(t *testing.T) {
	if statusPending != "pending" {
		t.Errorf("statusPending = %q, want %q", statusPending, "pending")
	}
	if statusSent != "sent" {
		t.Errorf("statusSent = %q, want %q", statusSent, "sent")
	}
	if statusFailed != "failed" {
		t.Errorf("statusFailed = %q, want %q", statusFailed, "failed")
	}
	if statusSkipped != "skipped" {
		t.Errorf("statusSkipped = %q, want %q", statusSkipped, "skipped")
	}
}

// TestNewService memverifikasi bahwa NewService membuat service dengan benar.
func TestNewService(t *testing.T) {
	log := slog.Default()
	loc, _ := time.LoadLocation("Asia/Jakarta")
	interval := 1 * time.Minute

	svc := New(nil, nil, loc, interval, 5, log)

	if svc == nil {
		t.Fatal("New() returned nil")
	}
	if svc.pollInterval != interval {
		t.Errorf("pollInterval = %v, want %v", svc.pollInterval, interval)
	}
	if svc.maxAttempts != 5 {
		t.Errorf("maxAttempts = %d, want %d", svc.maxAttempts, 5)
	}
	if svc.tz != loc {
		t.Errorf("tz = %v, want %v", svc.tz, loc)
	}
}
