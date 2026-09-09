package server

import (
	"context"
	"net/http"
	"time"

	"github.com/oktasatwika/sikons/internal/httpx"
)

// handleHealthz menjawab pertanyaan "proses ini masih hidup?".
//
// Sengaja TIDAK menyentuh database. Ini yang disebut liveness probe: kalau
// endpoint ini gagal, artinya prosesnya sendiri rusak dan harus di-restart.
// Kalau ikut mengecek database, database yang sedang lambat akan membuat
// orchestrator me-restart aplikasi yang sebenarnya sehat — restart yang tidak
// memperbaiki apa pun, hanya menambah downtime.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz menjawab pertanyaan "siap menerima trafik?".
//
// Ini readiness probe, dan di sini database MEMANG dicek: aplikasi tanpa
// database hidup, tapi tidak berguna. Bedanya dengan liveness: kalau ini
// gagal, aplikasi cukup dikeluarkan dari load balancer, tidak di-restart.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		s.log.Error("readiness gagal: database tidak merespons", "err", err)
		httpx.Error(w, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE",
			"Database sedang tidak dapat dihubungi", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
