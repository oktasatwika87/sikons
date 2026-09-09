package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/oktasatwika/sikons/internal/auth"
	"github.com/oktasatwika/sikons/internal/httpx"
)

// userResponse adalah bentuk user yang boleh dilihat dunia luar.
//
// Sengaja tipe terpisah dari auth.User, bukan mengembalikan struct domain
// langsung. Kalau suatu saat ada kolom baru yang sensitif ditambahkan ke
// auth.User, kolom itu tidak akan otomatis ikut bocor ke response — harus
// ditambahkan ke sini secara sadar. `password_hash` tidak pernah punya jalan
// untuk sampai ke sini.
type userResponse struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	FullName       string    `json:"full_name"`
	Role           string    `json:"role"`
	IdentityNumber *string   `json:"identity_number,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
}

func keUserResponse(u *auth.User) userResponse {
	return userResponse{
		ID: u.ID, Email: u.Email, FullName: u.FullName, Role: u.Role,
		IdentityNumber: u.IdentityNumber, IsActive: u.IsActive, CreatedAt: u.CreatedAt,
	}
}

type registerRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	FullName       string `json:"full_name"`
	IdentityNumber string `json:"identity_number"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Body request tidak valid: "+err.Error(), nil)
		return
	}

	// Perhatikan: tidak ada field `role` di registerRequest. Sekalipun klien
	// mengirim {"role":"admin"}, DecodeJSON akan menolaknya karena
	// DisallowUnknownFields — dan seandainya lolos pun, Register memaku
	// perannya jadi 'student'. Dua lapis, sengaja.
	u, err := s.auth.Register(r.Context(), auth.RegisterInput{
		Email:          req.Email,
		Password:       req.Password,
		FullName:       req.FullName,
		IdentityNumber: req.IdentityNumber,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			httpx.Error(w, http.StatusConflict, "EMAIL_TAKEN",
				"Email sudah terdaftar", nil)
		case errors.Is(err, auth.ErrPasswordTooShort),
			errors.Is(err, auth.ErrPasswordTooLong):
			httpx.Error(w, http.StatusUnprocessableEntity, httpx.CodeValidation,
				err.Error(), nil)
		default:
			// Error tak terduga dicatat lengkap di log, tapi yang sampai ke
			// pengguna cuma pesan generik — detail internal tidak boleh bocor.
			s.log.Error("register gagal", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{"user": keUserResponse(u)})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeValidation,
			"Body request tidak valid: "+err.Error(), nil)
		return
	}

	hasil, err := s.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			// 401 dengan pesan yang sengaja kabur. Membedakan "email tidak
			// terdaftar" dan "password salah" akan mengubah halaman login jadi
			// alat untuk mendata siapa saja yang punya akun.
			httpx.Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS",
				"Email atau password salah", nil)
		case errors.Is(err, auth.ErrAccountInactive):
			httpx.Error(w, http.StatusForbidden, "ACCOUNT_INACTIVE",
				"Akun ini dinonaktifkan", nil)
		default:
			s.log.Error("login gagal", "err", err)
			httpx.Internal(w)
		}
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"user":         keUserResponse(hasil.User),
		"access_token": hasil.AccessToken,
		"expires_at":   hasil.ExpiresAt,
	})
}

// handleMe membaca ulang user dari database, tidak sekadar mengembalikan isi
// token. Token cuma menyimpan id dan role, dan isinya adalah foto lama dari
// saat login — nama atau status aktif yang berubah setelah itu tidak tercermin.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		httpx.Internal(w)
		return
	}

	u, err := s.auth.ByID(r.Context(), id.UserID)
	if err != nil {
		// Token sah tapi usernya sudah tidak ada — akun dihapus setelah token
		// diterbitkan. Perlakukan seperti tidak terautentikasi.
		httpx.Error(w, http.StatusUnauthorized, httpx.CodeUnauthorized,
			"Akun tidak ditemukan", nil)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"user": keUserResponse(u)})
}
