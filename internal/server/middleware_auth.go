package server

import (
	"net/http"
	"strings"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/httpx"
)

// requireAuth membaca access token dari header Authorization dan menaruh
// identitas pemanggil ke dalam context request.
//
// Token dikirim lewat header, bukan cookie, dan itu disengaja: access token
// disimpan di memori frontend saja. Cookie akan ikut terkirim otomatis di
// setiap request lintas situs, yang membuka pintu CSRF. Cookie httpOnly nanti
// dipakai khusus untuk refresh token di M2b, karena refresh token justru TIDAK
// boleh bisa dibaca JavaScript.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized,
				"Butuh autentikasi", nil)
			return
		}

		// Pemisahan manual, bukan strings.TrimPrefix, supaya "Bearerabc" dan
		// "Bearer" tanpa token ikut ditolak.
		bagian := strings.Fields(header)
		if len(bagian) != 2 || !strings.EqualFold(bagian[0], "Bearer") {
			httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized,
				"Format header Authorization harus: Bearer <token>", nil)
			return
		}

		id, err := s.tokens.Parse(bagian[1])
		if err != nil {
			// Dibedakan supaya frontend tahu kapan harus diam-diam menukar
			// refresh token, dan kapan harus melempar user ke halaman login.
			kode := httpx.CodeUnauthorized
			pesan := "Token tidak valid"
			if err == auth.ErrTokenExpired {
				kode = "TOKEN_EXPIRED"
				pesan = "Token kedaluwarsa"
			}
			httpx.Error(w, http.StatusUnauthorized, kode, pesan, nil)
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
	})
}

// requireRole dipasang SETELAH requireAuth. Dia mengembalikan middleware,
// bukan menjadi middleware — itu supaya bisa dipanggil dengan argumen:
//
//	r.Use(s.requireRole(auth.RoleLecturer, auth.RoleAdmin))
//
// Pola "fungsi yang mengembalikan fungsi" ini yang membuat middleware di Go
// bisa dikonfigurasi tanpa perlu struct atau builder.
func (s *Server) requireRole(peran ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := auth.IdentityFrom(r.Context())
			if !ok {
				// Kalau ini terjadi, urutan middleware-nya salah pasang —
				// requireRole dipakai tanpa requireAuth di depannya. Itu bug
				// programmer, bukan kesalahan pengguna.
				s.log.Error("requireRole dipasang tanpa requireAuth",
					"path", r.URL.Path)
				httpx.Internal(w)
				return
			}

			for _, p := range peran {
				if id.Role == p {
					next.ServeHTTP(w, r)
					return
				}
			}

			// 403, bukan 404: pemanggilnya sudah terbukti siapa, cuma tidak
			// berhak. 401 juga salah — itu artinya "saya tidak tahu kamu siapa".
			httpx.Error(w, http.StatusForbidden, httpx.CodeForbidden,
				"Peran akun ini tidak diizinkan mengakses endpoint tersebut", nil)
		})
	}
}
