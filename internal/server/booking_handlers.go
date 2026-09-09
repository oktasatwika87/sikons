package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

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
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	Topic       string     `json:"topic"`
	Description string     `json:"description"`
	CreatedAt   string     `json:"created_at"`
	Slot        slotRef    `json:"slot"`
	Lecturer    *personRef `json:"lecturer,omitempty"`
	Student     *personRef `json:"student,omitempty"`
}

type slotRef struct {
	ID      string `json:"id"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type personRef struct {
	ID              string `json:"id"`
	FullName        string `json:"full_name"`
	Department      string `json:"department,omitempty"`
	IdentityNumber  string `json:"identity_number,omitempty"`
}

type bookingListResponse struct {
	Bookings []bookingResponse `json:"bookings"`
	Page     int              `json:"page"`
	PerPage  int              `json:"per_page"`
	Total    int              `json:"total"`
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

	// Validasi topic: wajib 3..200 karakter.
	req.Topic = strings.TrimSpace(req.Topic)
	if len(req.Topic) < 3 || len(req.Topic) > 200 {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"topic wajib 3 sampai 200 karakter", nil)
		return
	}

	// Validasi description: maksimal 2000 karakter.
	if len(req.Description) > 2000 {
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

	b, replay, err := s.booking.Create(r.Context(), booking.CreateInput{
		SlotID:          req.SlotID,
		StudentID:        id.UserID,
		Topic:            req.Topic,
		Description:      req.Description,
		IdempotencyKey:  idempotencyKey,
		RequestHash:      requestHash,
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
				"Minimal 60 menit sebelum konsultasi", nil)
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

	// Kalau replay, ambil booking yang sudah ada dan kembalikan.
	if replay && idempotencyKey != "" {
		// Ambil booking yang sudah ada berdasarkan slot_id dan student_id.
		var bookingID string
		err = s.db.QueryRow(r.Context(), `
			SELECT b.id::text FROM bookings b
			WHERE b.slot_id = $1::uuid AND b.student_id = $2::uuid
			ORDER BY b.created_at DESC LIMIT 1`,
			req.SlotID, id.UserID,
		).Scan(&bookingID)
		if err != nil {
			s.log.Warn("gagal ambil booking replay, generate dari b", "err", err)
			// Fallback: kalau b ada, pakai itu.
			if b != nil {
				w.Header().Set("Idempotency-Replayed", "true")
				httpx.JSON(w, http.StatusCreated, map[string]string{"id": b.ID})
				return
			}
			// Tidak ada booking, generate 500.
			s.log.Error("replay tanpa booking", "err", err)
			httpx.Internal(w)
			return
		}
		w.Header().Set("Idempotency-Replayed", "true")
		httpx.JSON(w, http.StatusCreated, map[string]string{"id": bookingID})
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]string{"id": b.ID})
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
	var bookings []booking.Booking
	var total int
	var err error

	switch id.Role {
	case auth.RoleStudent:
		bookings, total, err = s.booking.ListByStudent(r.Context(), id.UserID, status, page, perPage)
	case auth.RoleLecturer:
		bookings, total, err = s.booking.ListByLecturer(r.Context(), id.UserID, status, page, perPage)
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

	// Ambil detail slot dan person untuk setiap booking.
	responses := make([]bookingResponse, 0, len(bookings))
	for _, b := range bookings {
		resp, err := s.enrichBooking(r.Context(), &b, id.Role)
		if err != nil {
			s.log.Warn("gagal enrich booking", "booking_id", b.ID, "err", err)
			continue
		}
		responses = append(responses, *resp)
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

	b, err := s.booking.GetByID(r.Context(), bookingID)
	if err != nil {
		if errors.Is(err, booking.ErrSlotNotFound) {
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

	// Cek kepemilikan: mahasiswa hanya lihat miliknya, dosen lihat di slotnya.
	switch id.Role {
	case auth.RoleStudent:
		if b.StudentID != id.UserID {
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
			return
		}
	case auth.RoleLecturer:
		var lecturerID string
		err = s.db.QueryRow(r.Context(), `
			SELECT lecturer_id::text FROM slots WHERE id = $1::uuid`,
			b.SlotID,
		).Scan(&lecturerID)
		if err != nil || lecturerID != id.UserID {
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Booking tidak ditemukan", nil)
			return
		}
	default:
		httpx.Error(w, http.StatusForbidden, httpx.CodeForbidden,
			"Role ini tidak memiliki akses ke booking", nil)
		return
	}

	resp, err := s.enrichBooking(r.Context(), b, id.Role)
	if err != nil {
		s.log.Error("enrich booking", "err", err)
		httpx.Internal(w)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// enrichBooking mengambil detail slot dan person untuk satu booking.
func (s *Server) enrichBooking(ctx context.Context, b *booking.Booking, role string) (*bookingResponse, error) {
	var slotStart, slotEnd time.Time
	var lecturerID string
	err := s.db.QueryRow(ctx, `
		SELECT start_at, end_at, lecturer_id::text
		FROM slots WHERE id = $1::uuid`,
		b.SlotID,
	).Scan(&slotStart, &slotEnd, &lecturerID)
	if err != nil {
		return nil, fmt.Errorf("ambil slot: %w", err)
	}

	resp := bookingResponse{
		ID:          b.ID,
		Status:      b.Status,
		Topic:       b.Topic,
		Description: b.Description,
		CreatedAt:   b.CreatedAt.Format(time.RFC3339),
		Slot: slotRef{
			ID:      b.SlotID,
			StartAt: slotStart.Format(time.RFC3339),
			EndAt:   slotEnd.Format(time.RFC3339),
		},
	}

	switch role {
	case auth.RoleStudent:
		var fullName, department string
		err = s.db.QueryRow(ctx, `
			SELECT u.full_name, coalesce(lp.department, '')
			FROM users u
			JOIN lecturer_profiles lp ON lp.user_id = u.id
			WHERE u.id = $1::uuid`,
			lecturerID,
		).Scan(&fullName, &department)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("ambil dosen: %w", err)
		}
		resp.Lecturer = &personRef{
			ID:         lecturerID,
			FullName:   fullName,
			Department: department,
		}
	case auth.RoleLecturer:
		var fullName, identityNumber string
		err = s.db.QueryRow(ctx, `
			SELECT full_name, coalesce(identity_number, '')
			FROM users WHERE id = $1::uuid`,
			b.StudentID,
		).Scan(&fullName, &identityNumber)
		if err != nil {
			return nil, fmt.Errorf("ambil mahasiswa: %w", err)
		}
		resp.Student = &personRef{
			ID:             b.StudentID,
			FullName:       fullName,
			IdentityNumber: identityNumber,
		}
	}

	return &resp, nil
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
