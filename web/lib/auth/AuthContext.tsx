"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { apiFetch } from "@/lib/api/client";
import type {
  LoginResponse,
  RefreshResponse,
  UserResponse,
} from "@/lib/api/types";

/** Durasi minimum sebelum expires_at untuk memicu refresh proaktif (milidetik). */
const MARGIN_MS = 60 * 1000;

export interface AuthState {
  user: UserResponse | null;
  accessToken: string | null;
  expiresAt: Date | null;
  status: "loading" | "anonymous" | "authenticated";
}

interface AuthContextValue {
  auth: AuthState;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  /**
   * Refresh token secara manual: dipanggil oleh useApiFetch saat 401.
   * Mengembalikan `{ ok }` supaya pemanggil tidak perlu membaca `auth.status`
   * lewat closure (yang stale karena di-memoize dengan useCallback).
   */
  refreshTokens: () => Promise<RefreshResult>;
}

/** Hasil refresh. `accessToken` terisi kalau ok, dipakai bootstrap untuk /me. */
export interface RefreshResult {
  ok: boolean;
  accessToken?: string;
}

const AuthContext = createContext<AuthContextValue | null>(null);

/** ------------------------------------------------------------------ auth API */

async function callLogin(
  email: string,
  password: string
): Promise<LoginResponse> {
  return apiFetch<LoginResponse>("/auth/login", {
    method: "POST",
    body: { email, password },
  });
}

async function callRefresh(): Promise<RefreshResponse> {
  return apiFetch<RefreshResponse>("/auth/refresh", {
    method: "POST",
  });
}

// Backend mendaftarkan logout sebagai POST (internal/server/server.go).
// Dulu di sini DELETE, jadi backend membalas 405: refresh token tidak pernah
// dicabut dan cookie-nya tidak pernah dihapus.
async function callLogout(): Promise<void> {
  return apiFetch<void>("/auth/logout", { method: "POST" });
}

// GET /me butuh access token di header. Rute /auth/me tidak ada di backend.
async function callMe(accessToken: string): Promise<{ user: UserResponse }> {
  return apiFetch<{ user: UserResponse }>("/me", {
    headers: { Authorization: `Bearer ${accessToken}` },
  });
}

/** ------------------------------------------------------------------ AuthProvider */

interface AuthProviderProps {
  children: ReactNode;
}

export function AuthProvider({ children }: AuthProviderProps) {
  const [auth, setAuth] = useState<AuthState>({
    user: null,
    accessToken: null,
    expiresAt: null,
    status: "loading",
  });

  /**
   * Single-flight barrier untuk refresh.
   * Simpan promise refresh yang sedang berjalan: request lain yang datang
   * saat refresh belum selesai akan await promise yang sama, bukan memulai
   * refresh baru.
   */
  const refreshInFlight = useRef<Promise<RefreshResult> | null>(null);

  /** Perbarui state dari respons login. */
  const applyLogin = useCallback(
    (data: LoginResponse) => {
      setAuth({
        user: data.user,
        accessToken: data.access_token,
        expiresAt: new Date(data.expires_at),
        status: "authenticated",
      });
    },
    []
  );

  /** Set state jadi anonymous tanpa error: halaman publik tidak boleh error. */
  const setAnonymous = useCallback(() => {
    setAuth({
      user: null,
      accessToken: null,
      expiresAt: null,
      status: "anonymous",
    });
  }, []);

  /**
   * Refresh token dengan single-flight.
   * - Kalau refreshInFlight sudah ada, return promise yang sama.
   * - 409 REFRESH_IN_PROGRESS -> retry sekali setelah 500ms.
   * - Gagal final -> setAnonymous().
   *
   * Return `{ ok: boolean }` supaya pemanggil (useApiFetch) bisa memutuskan
   * langkah berikutnya tanpa harus membaca state `auth` lewat closure;
   * closure `auth` di useApiFetch stale karena fetchWithRetry di-memoize
   * pakai useCallback.
   */
  const refreshTokens = useCallback(
    async (): Promise<RefreshResult> => {
      if (refreshInFlight.current) {
        return refreshInFlight.current;
      }

      const promise = (async (): Promise<RefreshResult> => {
        // Pola retry saat dapat 409 REFRESH_IN_PROGRESS: jeda 500ms,
        // kalau masih 409 jeda 2000ms, baru setAnonymous kalau gagal
        // lagi. Backend bisa mengembalikan 409 saat rotasi "balasan
        // hilang" sedang diproses (cabang 2a): yaitu server commit
        // tapi cookie baru tidak sampai browser. Browser mengirim
        // ulang token lama, server butuh waktu untuk menyelesaikan
        // rotasi. Jeda 500ms + 2000ms = 2.5 detik total, cukup untuk
        // halaman di-render dan request kedua sampai.
        const sleep = (ms: number) =>
          new Promise((r) => setTimeout(r, ms));
        const is409Err = (err: unknown): boolean =>
          typeof err === "object" &&
          err !== null &&
          "code" in err &&
          (err as { code: string }).code === "REFRESH_IN_PROGRESS";

        const applySuccess = (data: { access_token: string; expires_at: string }) => {
          setAuth((prev) => ({
            ...prev,
            accessToken: data.access_token,
            expiresAt: new Date(data.expires_at),
          }));
          return data.access_token;
        };

        try {
          const data = await callRefresh();
          return { ok: true, accessToken: applySuccess(data) };
        } catch (err: unknown) {
          if (!is409Err(err)) {
            setAnonymous();
            return { ok: false };
          }

          // Retry pertama: 500ms.
          await sleep(500);
          try {
            const data = await callRefresh();
            return { ok: true, accessToken: applySuccess(data) };
          } catch (err2: unknown) {
            if (!is409Err(err2)) {
              setAnonymous();
              return { ok: false };
            }
          }

          // Retry kedua: 2000ms.
          await sleep(2000);
          try {
            const data = await callRefresh();
            return { ok: true, accessToken: applySuccess(data) };
          } catch {
            // Semua percobaan gagal: set anonymous.
            setAnonymous();
            return { ok: false };
          }
        } finally {
          refreshInFlight.current = null;
        }
      })();

      refreshInFlight.current = promise;
      return promise;
    },
    [setAnonymous]
  );

  /** Login: simpan token di memory, set state authenticated. */
  const login = useCallback(
    async (email: string, password: string) => {
      const data = await callLogin(email, password);
      applyLogin(data);
    },
    [applyLogin]
  );

  /** Logout: revoke token, hapus state. */
  const logout = useCallback(async () => {
    try {
      await callLogout();
    } finally {
      refreshInFlight.current = null;
      setAnonymous();
    }
  }, [setAnonymous]);
  /** ----------------------------------------------------------------
   * Bootstrap: pulihkan sesi dari cookie refresh.
   *
   * Access token cuma disimpan di memori, jadi selalu hilang saat halaman
   * dimuat ulang. Yang bertahan hanya cookie refresh (httpOnly, path
   * /api/v1/auth), maka urutannya: POST /auth/refresh dulu untuk dapat
   * access token baru, lalu GET /me dengan token itu untuk data user.
   *
   * Versi lama memanggil GET /auth/me, rute yang tidak pernah ada di backend,
   * sehingga setiap reload berakhir anonymous dan pengguna terlempar ke
   * /login walau cookie-nya masih sah.
   *
   * 401 saat bootstrap = kondisi normal "belum login": BUKAN error.
   * Jangan pernah set console.error di sini.
   */
  useEffect(() => {
    let cancelled = false;

    (async () => {
      // refreshTokens sudah single-flight dan sudah set anonymous sendiri
      // kalau gagal final, jadi cukup berhenti di sini.
      const r = await refreshTokens();
      if (cancelled || !r.ok || !r.accessToken) return;
      try {
        const { user } = await callMe(r.accessToken);
        if (cancelled) return;
        setAuth((prev) => ({ ...prev, user, status: "authenticated" }));
      } catch {
        if (!cancelled) setAnonymous();
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [refreshTokens, setAnonymous]);

  /** ----------------------------------------------------------------
   * Refresh proaktif terjadwal.
   * Menunggu sampai (expiresAt - MARGIN_MS), lalu refresh di background.
   * Kalau gagal, diam-diam set anonymous: halaman publik tidak boleh error.
   */
  useEffect(() => {
    if (auth.status !== "authenticated" || !auth.expiresAt) return;

    const delay = auth.expiresAt.getTime() - Date.now() - MARGIN_MS;
    if (delay <= 0) return;

    const timer = setTimeout(() => {
      refreshTokens().then((r) => {
        if (!r.ok) setAnonymous();
      });
    }, delay);

    return () => clearTimeout(timer);
  }, [auth.status, auth.expiresAt, refreshTokens, setAnonymous]);

  return (
    <AuthContext.Provider value={{ auth, login, logout, refreshTokens }}>
      {children}
    </AuthContext.Provider>
  );
}

/** ------------------------------------------------------------------ consumer */

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth harus dipakai di dalam AuthProvider");
  }
  return ctx;
}
