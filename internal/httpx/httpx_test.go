package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Bentuk amplop error adalah KONTRAK dengan frontend. Test ini yang menjaga
// supaya bentuknya tidak berubah tanpa sengaja saat refactor.
func TestBentukAmplopError(t *testing.T) {
	rec := httptest.NewRecorder()

	Error(rec, http.StatusConflict, "SLOT_ALREADY_BOOKED", "Slot sudah dipesan",
		map[string]any{"slot_id": "abc"})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, mau 409", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}

	var got struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}

	if got.Error.Code != "SLOT_ALREADY_BOOKED" {
		t.Errorf("code = %q", got.Error.Code)
	}
	if got.Error.Details["slot_id"] != "abc" {
		t.Errorf("details tidak terbawa: %v", got.Error.Details)
	}
}

// details kosong tidak boleh muncul sebagai "details": null di JSON.
func TestDetailsKosongDihilangkan(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, http.StatusNotFound, CodeNotFound, "Tidak ditemukan", nil)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw["error"], &inner); err != nil {
		t.Fatal(err)
	}
	if _, ada := inner["details"]; ada {
		t.Error(`field "details" seharusnya tidak ikut ditulis saat kosong`)
	}
}
