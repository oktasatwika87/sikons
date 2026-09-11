package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/booking"
	"github.com/oktasatwika/sikons/internal/httpx"
)

// ---------------------------------------------------------------- request/response DTO

type createBookingRequest struct {
	SlotID      string `json:"slot_id"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
	// StudentID sengaja tidak ada di sini — identitas pemanggil SELALU dari token.
}

type bookingResponse struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Topic        string     `json:"topic"`
	Description  string     `json:"description"`
	CreatedAt    string     `json:"created_at"`
	Slot         slotRef    `json:"slot"`
	Lecturer     *personRef `json:"lecturer,omitempty"`
	Student      *personRef `json:"student,omitempty"`
	LecturerNote string     `json:"lecturer_note,omitempty"`
}

type slotRef struct {
	ID      string `json:"id"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type cancelBookingRequest struct {
	Reason string `json:"reason"`
}

type completeBookingRequest struct {
	LecturerNote string `json:"lecturer_note"`
}

type personRef struct {
	ID             string `json:"id"`
	FullName       string `json:"full_name"`
	Department     string `json:"department,omitempty"`
	IdentityNumber string `json:"identity_number,omitempty"`
}

type bookingListResponse struct {
	Bookings []bookingResponse `json:"bookings"`
	Page     int               `json:"page"`
	PerPage  int               `json:"per_page"`
	Total    int               `json:"total"`
}

// ---------------------------------------------------------------- handler

func (s *Server) handleCreateBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	// Pastikan student_id tidak masuk dari body.
	var req createBookingRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Body request tidak valid: "+err.Error(), nil)
		return
	}

	// Validasi slot_id sebagai UUID.
	if !isValidUUID(req.SlotID) {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"slot_id bukan UUID yang valid", nil)
		return
	}

	// Validasi topic: wajib 3..200 karakter (diukur dalam RUNA, bukan byte —
	// satu emoji harus dihitung satu karakter).
	req.Topic = strings.TrimSpace(req.Topic)
	topicRunes := utf8.RuneCountInString(req.Topic)
	if topicRunes < 3 || topicRunes > 200 {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"topic wajib 3 sampai 200 karakter", nil)
		return
	}

	// Validasi description: maksimal 2000 karakter.
	if utf8.RuneCountInString(req.Description) > 2000 {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"description maksimal 2000 karakter", nil)
		return
	}

	// Idempotency-Key opsional. Kalau ada: panjang 8..255.
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey != "" {
		if len(idempotencyKey) < 8 || len(idempotencyKey) > 255 {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"Idempotency-Key wajib 8 sampai 255 karakter", nil)
			return
		}
	}

	// ComputeRequestHash dari field yang sudah di-decode dan di-trim.
	requestHash := booking.ComputeRequestHash(req.SlotID, req.Topic, req.Description)

	result, err := s.booking.Create(r.Context(), booking.CreateInput{
		SlotID:         req.SlotID,
		StudentID:      id.UserID,
		Topic:          req.Topic,
		Description:    req.Description,
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
	})
	if err != nil {
		switch {
		case errors.Is(err, booking.ErrSlotNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Slot tidak ditemukan", nil)
		case errors.Is(err, booking.ErrSlotWithdrawn):
			httpx.Error(w, http.StatusConflict, "SLOT_WITHDRAWN",
				"Slot sudah tidak tersedia", nil)
		case errors.Is(err, booking.ErrSlotAlreadyBooked):
			httpx.Error(w, http.StatusConflict, "SLOT_ALREADY_BOOKED",
				"Slot sudah dipesan orang lain", nil)
		case errors.Is(err, booking.ErrLimitReached):
			httpx.Error(w, http.StatusConflict, "BOOKING_LIMIT_REACHED",
				"Batas 3 booking aktif sudah tercapai", nil)
		case errors.Is(err, booking.ErrSlotTooSoon):
			httpx.Error(w, http.StatusUnprocessableEntity, "SLOT_TOO_SOON",
				fmt.Sprintf("Minimal %d menit sebelum konsultasi",
					s.booking.MinLeadMinutes()), nil)
		case errors.Is(err, booking.ErrIdempotencyReused):
			httpx.Error(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED",
				"Idempotency-Key sudah dipakai dengan request berbeda", nil)
		case errors.Is(err, booking.ErrStudentNotFound):
			// Token valid tapi user sudah tidak ada atau nonaktif.
			s.log.Warn("booking oleh user nonaktif atau bukan mahasiswa",
				"user_id", id.UserID, "role", id.Role)
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized,
				"Akun tidak valid", nil)
		default:
			s.log.Error("membuat booking", "err", err)
			httpx.Internal(w)
		}
		return
	}

	if result.Replay {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	// Body sudah jadi JSON yang valid (disusun di service).
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(result.Status)
	_, _ = w.Write(result.Body)
}

func (s *Server) handleListBookings(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	// Query parameter.
	status := r.URL.Query().Get("status")
	if status != "" {
		switch status {
		case "confirmed", "cancelled", "completed", "no_show":
			// OK.
		default:
			httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
				"status harus salah satu: confirmed, cancelled, completed, no_show", nil)
			return
		}
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	perPage := 20
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if n, err := strconv.Atoi(pp); err == nil && n > 0 {
			perPage = n
			if perPage > 100 {
				perPage = 100
			}
		}
	}

	// Role dari token, bukan dari query.
	// Menerima role dari query membuka pintu yang tidak perlu ada:
	// frontend tidak butuh fitur "lihat booking orang lain atas nama admin".
	var views []booking.BookingView
	var total int
	var err error

	switch id.Role {
	case auth.RoleStudent:
		views, total, err = s.booking.ListByStudent(r.Context(), id.UserID, status, page, perPage)
	case auth.RoleLecturer:
		views, total, err = s.booking.ListByLecturer(r.Context(), id.UserID, status, page, perPage)
	default:
		httpx.Error(w, http.StatusForbidden, httpx.CodeForbidden,
			"Role ini tidak memiliki booking", nil)
		return
	}

	if err != nil {
		s.log.Error("mengambil daftar booking", "err", err)
		httpx.Internal(w)
		return
	}

	responses := make([]bookingResponse, 0, len(views))
	for i := range views {
		responses = append(responses, viewToResponse(&views[i], id.Role))
	}

	httpx.JSON(w, http.StatusOK, bookingListResponse{
		Bookings: responses,
		Page:     page,
		PerPage:  perPage,
		Total:    total,
	})
}

func (s *Server) handleGetBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID booking wajib diisi", nil)
		return
	}

	view, err := s.booking.GetByID(r.Context(), bookingID, id.UserID, id.Role)
	if err != nil {
		if errors.Is(err, booking.ErrBookingNotFound) {
			// Tidak ada bedanya 404 antara "tidak ada" dan "milik orang lain".
			// Ini konsisten dengan keputusan di availability handlers.
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
			return
		}
		s.log.Error("mengambil booking", "err", err)
		httpx.Internal(w)
		return
	}

	httpx.JSON(w, http.StatusOK, viewToResponse(view, id.Role))
}

// viewToResponse menerjemahkan BookingView jadi DTO HTTP sesuai peran pemanggil.
// field person yang tidak relevan (mahasiswa untuk student, dosen untuk lecturer)
// sengaja tidak dimunculkan agar klien tidak bingung mana yang harus dipakai.
func viewToResponse(v *booking.BookingView, role string) bookingResponse {
	resp := bookingResponse{
		ID:           v.ID,
		Status:       v.Status,
		Topic:        v.Topic,
		Description:  v.Description,
		CreatedAt:    v.CreatedAt.Format(time.RFC3339),
		LecturerNote: v.LecturerNote,
		Slot: slotRef{
			ID:      v.SlotID,
			StartAt: v.SlotStart.Format(time.RFC3339),
			EndAt:   v.SlotEnd.Format(time.RFC3339),
		},
	}
	switch role {
	case auth.RoleStudent:
		resp.Lecturer = &personRef{
			ID:         v.LecturerID,
			FullName:   v.LecturerFullName,
			Department: v.LecturerDepartment,
		}
	case auth.RoleLecturer:
		resp.Student = &personRef{
			ID:             v.StudentID,
			FullName:       v.StudentFullName,
			IdentityNumber: v.StudentIdentity,
		}
	}
	return resp
}

// isValidUUID memvalidasi format UUID.
// UUID valid: 36 karakter dengan 4 hyphen di posisi 9, 14, 19, 24.
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	// Posisi hyphen: 8, 13, 18, 23 (0-indexed).
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	// Periksa karakter hex lainnya.
	for _, c := range s {
		if c == '-' {
			continue
		}
		if !isHexChar(byte(c)) {
			return false
		}
	}
	return true
}

func isHexChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (s *Server) handleCancelBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID booking wajib diisi", nil)
		return
	}

	// Body opsional: { "reason": "..." }
	var req cancelBookingRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
				"Body request tidak valid: "+err.Error(), nil)
			return
		}
		// Validasi panjang reason.
		if utf8.RuneCountInString(req.Reason) > 500 {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"reason maksimal 500 karakter", nil)
			return
		}
	}

	view, err := s.booking.Cancel(r.Context(), bookingID, id.UserID, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, booking.ErrBookingNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
		case errors.Is(err, booking.ErrBookingAlreadyCancelled):
			httpx.Error(w, http.StatusConflict, "BOOKING_ALREADY_CANCELLED",
				"Booking sudah dibatalkan", nil)
		case errors.Is(err, booking.ErrBookingAlreadyFinalized):
			httpx.Error(w, http.StatusConflict, "BOOKING_ALREADY_FINALIZED",
				"Booking sudah diselesaikan", nil)
		case errors.Is(err, booking.ErrCancelTooLate):
			httpx.Error(w, http.StatusUnprocessableEntity, "CANCEL_TOO_LATE",
				fmt.Sprintf("Pembatalan wajib minimal %d jam sebelum jadwal",
					s.booking.CancelMinHours()), nil)
		default:
			s.log.Error("membatalkan booking", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusOK, viewToResponse(view, id.Role))
}

func (s *Server) handleCompleteBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID booking wajib diisi", nil)
		return
	}

	// Body opsional: { "lecturer_note": "..." }
	var req completeBookingRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
				"Body request tidak valid: "+err.Error(), nil)
			return
		}
		// Validasi panjang note.
		if utf8.RuneCountInString(req.LecturerNote) > 2000 {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"lecturer_note maksimal 2000 karakter", nil)
			return
		}
	}

	view, err := s.booking.Complete(r.Context(), bookingID, id.UserID, req.LecturerNote)
	if err != nil {
		switch {
		case errors.Is(err, booking.ErrBookingNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
		case errors.Is(err, booking.ErrBookingNotConfirmed):
			httpx.Error(w, http.StatusConflict, "BOOKING_NOT_CONFIRMED",
				"Booking belum dikonfirmasi", nil)
		case errors.Is(err, booking.ErrSessionNotStarted):
			httpx.Error(w, http.StatusUnprocessableEntity, "SESSION_NOT_STARTED",
				"Sesi belum dimulai", nil)
		default:
			s.log.Error("menyelesaikan booking", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusOK, viewToResponse(view, id.Role))
}

func (s *Server) handleNoShowBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID booking wajib diisi", nil)
		return
	}

	view, err := s.booking.NoShow(r.Context(), bookingID, id.UserID)
	if err != nil {
		switch {
		case errors.Is(err, booking.ErrBookingNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
		case errors.Is(err, booking.ErrBookingNotConfirmed):
			httpx.Error(w, http.StatusConflict, "BOOKING_NOT_CONFIRMED",
				"Booking belum dikonfirmasi", nil)
		case errors.Is(err, booking.ErrSessionNotStarted):
			httpx.Error(w, http.StatusUnprocessableEntity, "SESSION_NOT_STARTED",
				"Sesi belum dimulai", nil)
		default:
			s.log.Error("menandai no-show", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusOK, viewToResponse(view, id.Role))
}
