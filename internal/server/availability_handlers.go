package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/availability"
	"github.com/oktasatwika/sikons/internal/httpx"
)

// ---------------------------------------------------------------- availability rules

type createRuleRequest struct {
	DayOfWeek       int    `json:"day_of_week"`
	StartTime       string `json:"start_time"` // HH:MM
	EndTime         string `json:"end_time"`   // HH:MM
	SlotDurationMin int    `json:"slot_duration_min"`
	EffectiveFrom   string `json:"effective_from"`         // YYYY-MM-DD
	EffectiveTo     string `json:"effective_to,omitempty"` // YYYY-MM-DD
}

type updateRuleRequest struct {
	EffectiveTo string `json:"effective_to,omitempty"` // YYYY-MM-DD
	IsActive    *bool  `json:"is_active,omitempty"`
}

func (s *Server) handleCreateAvailabilityRule(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	var req createRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Body request tidak valid: "+err.Error(), nil)
		return
	}

	// Validasi field yang tidak boleh nol
	if req.DayOfWeek < 0 || req.DayOfWeek > 6 {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"day_of_week harus antara 0 (Minggu) dan 6 (Sabtu)", nil)
		return
	}
	if req.SlotDurationMin < 15 || req.SlotDurationMin > 180 {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"slot_duration_min harus antara 15 dan 180", nil)
		return
	}
	if req.EffectiveFrom == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"effective_from wajib diisi", nil)
		return
	}

	ruleID, err := s.availability.CreateRule(r.Context(), availability.CreateRuleInput{
		LecturerID:      id.UserID,
		DayOfWeek:       req.DayOfWeek,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		SlotDurationMin: req.SlotDurationMin,
		EffectiveFrom:   req.EffectiveFrom,
		EffectiveTo:     req.EffectiveTo,
	})
	if err != nil {
		switch {
		case errors.Is(err, availability.ErrInvalidTimeOrder):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"waktu selesai harus setelah waktu mulai", nil)
		case errors.Is(err, availability.ErrWindowTooSmall):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"jendela waktu lebih pendek dari durasi slot", nil)
		case errors.Is(err, availability.ErrInvalidDateOrder):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"tanggal selesai harus setelah atau sama dengan tanggal mulai", nil)
		case errors.Is(err, availability.ErrRuleConflict):
			httpx.Error(w, http.StatusConflict, "ATURAN_BENTROK",
				"Sudah ada aturan aktif di hari dan rentang jam yang sama", nil)
		default:
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if pgErr.Code == "23514" { // check_violation
					httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
						"Jendela waktu tidak muat dalam durasi slot", nil)
					return
				}
			}
			s.log.Error("membuat aturan ketersediaan", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]string{"id": ruleID})
}

func (s *Server) handleListAvailabilityRules(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	rules, err := s.availability.GetRulesByLecturer(r.Context(), id.UserID)
	if err != nil {
		s.log.Error("mengambil aturan ketersediaan", "err", err)
		httpx.Internal(w)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleUpdateAvailabilityRule(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	ruleID := chi.URLParam(r, "id")
	if ruleID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID aturan wajib diisi", nil)
		return
	}

	var req updateRuleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"Field tidak diizinkan: "+err.Error(), nil)
		return
	}

	// Validasi effective_to kalau ada
	if req.EffectiveTo != "" {
		if _, err := time.Parse("2006-01-02", req.EffectiveTo); err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"effective_to harus dalam format YYYY-MM-DD", nil)
			return
		}
	}

	err := s.availability.UpdateRule(r.Context(), ruleID, id.UserID, availability.UpdateRuleInput{
		EffectiveTo: &req.EffectiveTo,
		IsActive:    req.IsActive,
	})
	if err != nil {
		switch {
		case errors.Is(err, availability.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Aturan tidak ditemukan", nil)
		default:
			s.log.Error("mengubah aturan ketersediaan", "err", err)
			httpx.Internal(w)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAvailabilityRule(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	ruleID := chi.URLParam(r, "id")
	if ruleID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID aturan wajib diisi", nil)
		return
	}

	err := s.availability.DeleteRule(r.Context(), ruleID, id.UserID)
	if err != nil {
		switch {
		case errors.Is(err, availability.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Aturan tidak ditemukan", nil)
		default:
			s.log.Error("menghapus aturan ketersediaan", "err", err)
			httpx.Internal(w)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- availability exceptions

type createExceptionRequest struct {
	ExceptionDate string `json:"exception_date"` // YYYY-MM-DD
	Reason        string `json:"reason"`
	IsFullDay     bool   `json:"is_full_day"`
	StartTime     string `json:"start_time,omitempty"` // HH:MM
	EndTime       string `json:"end_time,omitempty"`   // HH:MM
}

func (s *Server) handleCreateAvailabilityException(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	var req createExceptionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Body request tidak valid: "+err.Error(), nil)
		return
	}

	if req.ExceptionDate == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"exception_date wajib diisi", nil)
		return
	}
	if _, err := time.Parse("2006-01-02", req.ExceptionDate); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"exception_date harus dalam format YYYY-MM-DD", nil)
		return
	}
	if req.Reason == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"reason wajib diisi", nil)
		return
	}

	exceptionID, err := s.availability.CreateException(r.Context(), availability.CreateExceptionInput{
		LecturerID:    id.UserID,
		ExceptionDate: req.ExceptionDate,
		Reason:        req.Reason,
		IsFullDay:     req.IsFullDay,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
	})
	if err != nil {
		switch {
		case errors.Is(err, availability.ErrInvalidException):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"Pengecualian parsial harus memiliki jam mulai dan selesai", nil)
		case errors.Is(err, availability.ErrInvalidTimeOrder):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"Waktu selesai harus setelah waktu mulai", nil)
		default:
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if pgErr.Code == "23514" { // check_violation
					httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
						"Pengecualian tidak valid", nil)
					return
				}
			}
			s.log.Error("membuat pengecualian ketersediaan", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]string{"id": exceptionID})
}

func (s *Server) handleListAvailabilityExceptions(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	if from == "" || to == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Parameter from dan to wajib diisi", nil)
		return
	}

	if _, err := time.Parse("2006-01-02", from); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"Parameter from harus dalam format YYYY-MM-DD", nil)
		return
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"Parameter to harus dalam format YYYY-MM-DD", nil)
		return
	}

	exceptions, err := s.availability.GetExceptionsByLecturer(r.Context(), id.UserID, from, to)
	if err != nil {
		s.log.Error("mengambil pengecualian ketersediaan", "err", err)
		httpx.Internal(w)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"exceptions": exceptions})
}

func (s *Server) handleDeleteAvailabilityException(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	exceptionID := chi.URLParam(r, "id")
	if exceptionID == "" {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"ID pengecualian wajib diisi", nil)
		return
	}

	err := s.availability.DeleteException(r.Context(), exceptionID, id.UserID)
	if err != nil {
		switch {
		case errors.Is(err, availability.ErrNotFound):
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Pengecualian tidak ditemukan", nil)
		default:
			s.log.Error("menghapus pengecualian ketersediaan", "err", err)
			httpx.Internal(w)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
