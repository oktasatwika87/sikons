package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/oktasatwika87/sikons/internal/auth"
	"github.com/oktasatwika87/sikons/internal/httpx"
)

// ---------------------------------------------------------------- response types

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

// ---------------------------------------------------------------- cookie

const cookieRefreshToken = "refresh_token"

func cookieRefreshTokenBaru(token string, maxAge int, isDev bool) *http.Cookie {
	return &http.Cookie{
		Name:     cookieRefreshToken,
		Value:    token,
		Path:     "/api/v1/auth",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   !isDev,
	}
}

func hapusCookieRefreshToken() *http.Cookie {
	return &http.Cookie{
		Name:     cookieRefreshToken,
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// ---------------------------------------------------------------- register

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

// ---------------------------------------------------------------- login

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

	hasil, err := s.auth.Login(r.Context(), req.Email, req.Password, r.UserAgent())
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

	// Set refresh token sebagai httpOnly cookie.
	http.SetCookie(w, cookieRefreshTokenBaru(
		hasil.RefreshToken,
		int(auth.RefreshTokenTTL.Seconds()),
		s.cfg.IsDevelopment(),
	))

	httpx.JSON(w, http.StatusOK, map[string]any{
		"user":               keUserResponse(hasil.User),
		"access_token":       hasil.AccessToken,
		"expires_at":         hasil.ExpiresAt,
		"refresh_expires_at": hasil.RefreshExpires,
	})
}

// ---------------------------------------------------------------- refresh

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieRefreshToken)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID",
			"Sesi habis, silakan login ulang", nil)
		return
	}

	plaintextBaru, userID, err := s.auth.RotateRefreshToken(r.Context(), cookie.Value, r.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRefreshTokenInvalid):
			s.log.Warn("refresh token tidak ditemukan", "err", err)
			httpx.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID",
				"Sesi habis, silakan login ulang", nil)
		case errors.Is(err, auth.ErrRefreshTokenExpired):
			s.log.Warn("refresh token kedaluwarsa", "err", err)
			httpx.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_EXPIRED",
				"Sesi habis, silakan login ulang", nil)
		case errors.Is(err, auth.ErrRefreshInProgress):
			// Balapan wajar — jangan set cookie, kembalikan 409.
			// Frontend harus retry setelah sesaat.
			httpx.Error(w, http.StatusConflict, "REFRESH_IN_PROGRESS",
				"Sesi sedang diperbarui, coba lagi sesaat lagi", nil)
		case errors.Is(err, auth.ErrRefreshTokenReused):
			s.log.Warn("refresh token reuse terdeteksi — kemungkinan pencurian",
				"user_agent", r.UserAgent())
			httpx.Error(w, http.StatusUnauthorized, "REFRESH_TOKEN_REUSED",
				"Sesi dicabut karena aktivitas mencurigakan, silakan login ulang", nil)
		default:
			s.log.Error("refresh token gagal", "err", err)
			httpx.Internal(w)
		}
		return
	}

	u, err := s.auth.ByID(r.Context(), userID)
	if err != nil {
		s.log.Error("membaca user", "err", err)
		httpx.Internal(w)
		return
	}

	accessToken, expiresAt, err := s.tokens.Issue(auth.Identity{UserID: u.ID, Role: u.Role})
	if err != nil {
		s.log.Error("menerbitkan access token", "err", err)
		httpx.Internal(w)
		return
	}

	// Set cookie baru.
	http.SetCookie(w, cookieRefreshTokenBaru(
		plaintextBaru,
		int(auth.RefreshTokenTTL.Seconds()),
		s.cfg.IsDevelopment(),
	))

	httpx.JSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"expires_at":   expiresAt,
	})
}

// ---------------------------------------------------------------- logout

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieRefreshToken)
	if err == nil {
		if revokeErr := s.auth.RevokeRefreshToken(r.Context(), cookie.Value); revokeErr != nil {
			s.log.Error("logout: mencabut refresh token", "err", revokeErr)
			// Tetap lanjut — cookie akan dihapus meski token gagal dicabut.
		}
	}

	http.SetCookie(w, hapusCookieRefreshToken())
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- /me

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
