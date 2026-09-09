package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// requestLogger mencatat setiap request dalam format terstruktur.
//
// Kenapa bukan sekadar fmt.Println: log terstruktur bisa DI-QUERY. Saat nanti
// di VPS kamu ingin tahu "semua request yang balas 409 dalam 10 menit
// terakhir", teks bebas memaksamu memakai grep dan regex; JSON dengan field
// `status` bisa disaring langsung.
//
// Bentuk fungsi ini — menerima http.Handler dan mengembalikan http.Handler —
// adalah pola middleware standar Go. Tidak ada yang ajaib: setiap middleware
// membungkus handler berikutnya seperti lapisan bawang.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// chi membungkus ResponseWriter supaya status code dan jumlah byte
		// bisa dibaca SETELAH handler selesai. http.ResponseWriter bawaan
		// tidak menyimpan status yang sudah ditulis.
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			s.log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		}()

		// defer di atas tetap jalan meski handler ini panic, jadi request yang
		// gagal total pun tetap tercatat.
		next.ServeHTTP(ww, r)
	})
}
