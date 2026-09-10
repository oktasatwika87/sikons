package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/oktasatwika/sikons/internal/httpx"
	"github.com/oktasatwika/sikons/internal/lecturer"
)

// handleListLecturers menampilkan daftar dosen.
// Endpoint ini PUBLIK karena:
// - Browsing daftar dosen bukan data sensitif.
// - Portfolio ini butuh bisa dilihat recruiter yang belum login.
// - Tidak ada aksi destruktif yang bisa dilakukan tanpa auth.
func (s *Server) handleListLecturers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := 1
	if p := q.Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	perPage := 20
	if pp := q.Get("per_page"); pp != "" {
		if n, err := strconv.Atoi(pp); err == nil && n > 0 {
			perPage = n
			if perPage > 100 {
				perPage = 100
			}
		}
	}

	params := lecturer.ListParams{
		Department: q.Get("department"),
		Q:          q.Get("q"),
		Page:       page,
		PerPage:    perPage,
	}

	result, err := s.lecturer.List(r.Context(), params)
	if err != nil {
		s.log.Error("list lecturers", "err", err, "params", params)
		httpx.Internal(w)
		return
	}

	if result.Lecturers == nil {
		result.Lecturers = []lecturer.Lecturer{}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"lecturers": result.Lecturers,
		"page":      result.Page,
		"per_page":  result.PerPage,
		"total":     result.Total,
	})
}

// handleGetLecturer menampilkan detail satu dosen.
// Endpoint ini PUBLIK — alasan sama dengan handleListLecturers.
func (s *Server) handleGetLecturer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	l, err := s.lecturer.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, lecturer.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Dosen tidak ditemukan", nil)
			return
		}
		s.log.Error("get lecturer", "err", err)
		httpx.Internal(w)
		return
	}

	httpx.JSON(w, http.StatusOK, l)
}

// handleGetLecturerSlots menampilkan slot yang tersedia untuk satu dosen.
// Endpoint ini PUBLIK — melihat kalender kosong dosen tidak butuh login.
// Yang butuh login baru POST /bookings.
func (s *Server) handleGetLecturerSlots(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q := r.URL.Query()

	// Validasi dosen ada
	_, err := s.lecturer.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, lecturer.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, httpx.CodeNotFound,
				"Dosen tidak ditemukan", nil)
			return
		}
		s.log.Error("get lecturer for slots", "err", err)
		httpx.Internal(w)
		return
	}

	// Parse rentang tanggal
	now := time.Now().UTC()

	var from, to time.Time
	if fromStr := q.Get("from"); fromStr != "" {
		var err error
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"Parameter 'from' harus format RFC3339", nil)
			return
		}
	} else {
		from = now
	}

	if toStr := q.Get("to"); toStr != "" {
		var err error
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				"Parameter 'to' harus format RFC3339", nil)
			return
		}
	} else {
		to = from.AddDate(0, 0, 7)
	}

	// Validasi rentang
	if !to.After(from) {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"Parameter 'to' harus setelah 'from'", nil)
		return
	}

	maxRange := time.Duration(s.slotHorizon) * 24 * time.Hour
	if to.Sub(from) > maxRange {
		httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
			"Rentang tanggal tidak boleh lebih dari "+strconv.Itoa(s.slotHorizon)+" hari", nil)
		return
	}

	slots, err := s.lecturer.GetSlots(r.Context(), id, from, to)
	if err != nil {
		s.log.Error("get lecturer slots", "err", err)
		httpx.Internal(w)
		return
	}

	if slots == nil {
		slots = []lecturer.Slot{}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"lecturer_id": id,
		"from":        from.Format(time.RFC3339),
		"to":          to.Format(time.RFC3339),
		"slots":       slots,
	})
}
