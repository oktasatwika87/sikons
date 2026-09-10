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
   * Refresh token secara manual — dipanggil oleh useApiFetch saat 401.
   * Mengembalikan `{ ok }` supaya pemanggil tidak perlu membaca `auth.status`
   * lewat closure (yang stale karena di-memoize dengan useCallback).
   */
  refreshTokens: () => Promise<{ ok: boolean }>;
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

async function callLogout(): Promise<void> {
  return apiFetch<void>("/auth/logout", { method: "DELETE" });
}

async function callMe(): Promise<{ user: UserResponse }> {
  return apiFetch<{ user: UserResponse }>("/auth/me");
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
   * Simpan promise refresh yang sedang berjalan — request lain yang datang
   * saat refresh belum selesai akan await promise yang sama, bukan memulai
   * refresh baru.
   */
  const refreshInFlight = useRef<Promise<{ ok: boolean }> | null>(null);

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

  /** Set state jadi anonymous tanpa error — halaman publik tidak boleh error. */
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
   * - 409 REFRESH_IN_PROGRESS → retry sekali setelah 500ms.
   * - Gagal final → setAnonymous().
   *
   * Return `{ ok: boolean }` supaya pemanggil (useApiFetch) bisa memutuskan
   * langkah berikutnya tanpa harus membaca state `auth` lewat closure —
   * closure `auth` di useApiFetch stale karena fetchWithRetry di-memoize
   * pakai useCallback.
   */
  const refreshTokens = useCallback(
    async (): Promise<{ ok: boolean }> => {
      if (refreshInFlight.current) {
        return refreshInFlight.current;
      }

      const promise = (async (): Promise<{ ok: boolean }> => {
        try {
          const data = await callRefresh();
          setAuth((prev) => ({
            ...prev,
            accessToken: data.access_token,
            expiresAt: new Date(data.expires_at),
          }));
          return { ok: true };
        } catch (err: unknown) {
          // 409 → tunggu sebentar lalu retry sekali
          const is409 =
            typeof err === "object" &&
            err !== null &&
            "code" in err &&
            (err as { code: string }).code === "REFRESH_IN_PROGRESS";

          if (is409) {
            await new Promise((r) => setTimeout(r, 500));
            try {
              const data = await callRefresh();
              setAuth((prev) => ({
                ...prev,
                accessToken: data.access_token,
                expiresAt: new Date(data.expires_at),
              }));
              return { ok: true };
            } catch {
              setAnonymous();
              return { ok: false };
            }
          } else {
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

  /** Login — simpan token di memory, set state authenticated. */
  const login = useCallback(
    async (email: string, password: string) => {
      const data = await callLogin(email, password);
      applyLogin(data);
    },
    [applyLogin]
  );

  /** Logout — revoke token, hapus state. */
  const logout = useCallback(async () => {
    try {
      await callLogout();
    } finally {
      refreshInFlight.current = null;
      setAnonymous();
    }
  }, [setAnonymous]);
  /** ----------------------------------------------------------------
   * Bootstrap: coba /auth/me untuk dapat token yang sudah ada di cookie.
   *
   * 401 saat bootstrap = kondisi normal "belum login" — BUKAN error.
   * Jangan pernah set console.error di sini.
   */
  useEffect(() => {
    let cancelled = false;

    callMe()
      .then(({ user }) => {
        if (cancelled) return;
        // /auth/me tidak kasih token/expires, jadi bootstrap dengan
        // expiresAt null — refresh proaktif akan aktif setelah request
        // pertama (useApiFetch) yang kena 401.
        setAuth({
          user,
          accessToken: null,
          expiresAt: null,
          status: "authenticated",
        });
      })
      .catch(() => {
        if (cancelled) return;
        setAnonymous();
      });

    return () => {
      cancelled = true;
    };
  }, [setAnonymous]);

  /** ----------------------------------------------------------------
   * Refresh proaktif terjadwal.
   * Menunggu sampai (expiresAt - MARGIN_MS), lalu refresh di background.
   * Kalau gagal, diam-diam set anonymous — halaman publik tidak boleh error.
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
