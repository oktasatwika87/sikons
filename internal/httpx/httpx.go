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
	"errors"
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
	CodeConflict     = "CONFLICT"
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

// batasBodyRequest mencegah satu request mengirim JSON 2 GB dan menghabiskan
// memori server. Angkanya sengaja kecil: tidak ada endpoint di API ini yang
// perlu mengirim lebih dari beberapa kilobyte.
const batasBodyRequest = 64 << 10 // 64 KiB

// DecodeJSON membaca body request ke dalam dst.
//
// DisallowUnknownFields dinyalakan supaya field yang tidak dikenal DITOLAK,
// bukan diabaikan diam-diam. Ini menangkap salah ketik dari sisi klien
// ("fullname" vs "full_name") saat itu juga, alih-alih membiarkan field itu
// kosong lalu bingung kenapa datanya hilang.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, batasBodyRequest)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Body harus berisi TEPAT satu objek JSON. Tanpa pemeriksaan ini,
	// `{"a":1}{"b":2}` diterima dan objek keduanya hilang tanpa jejak.
	if err := dec.Decode(&struct{}{}); err == nil {
		return errors.New("body harus berisi satu objek JSON")
	}
	return nil
}
