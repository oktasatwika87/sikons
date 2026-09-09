// Package httpx berisi helper untuk menulis response.
//
// Semua response error di API ini punya bentuk yang sama:
//
//	{ "error": { "code": "SLOT_ALREADY_BOOKED", "message": "...", "details": {} } }
//
// `code` adalah kontrak untuk mesin (frontend mencabangkan logika di sini,
// dan nilainya TIDAK BOLEH berubah sembarangan), `message` untuk manusia.
// Memisahkan keduanya berarti teks pesan bisa diubah kapan saja tanpa
// merusak frontend.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// Kode error yang sudah pasti dipakai. Dikumpulkan sebagai konstanta supaya
// salah ketik ketahuan saat compile, bukan saat frontend kebingungan.
const (
	CodeValidation   = "VALIDATION_ERROR"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"
	CodeInternal     = "INTERNAL_ERROR"
)

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// Header sudah terkirim, jadi status tidak bisa diubah lagi.
		// Yang bisa dilakukan cuma mencatatnya.
		slog.Error("gagal menulis response JSON", "err", err)
	}
}

func Error(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	JSON(w, status, errorEnvelope{Error: ErrorBody{
		Code:    code,
		Message: message,
		Details: details,
	}})
}

func Internal(w http.ResponseWriter) {
	// Pesan sengaja generik: detail error internal tidak boleh bocor ke
	// pengguna. Detail aslinya sudah masuk log.
	Error(w, http.StatusInternalServerError, CodeInternal, "Terjadi kesalahan pada server", nil)
}
