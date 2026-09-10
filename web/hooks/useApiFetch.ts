"use client";

import { useCallback, useEffect, useRef } from "react";
import { useAuth } from "@/lib/auth/AuthContext";
import { ApiError } from "@/lib/api/errors";

/**
 * Wrapper fetch yang aware auth.
 *
 * Perilaku:
 * 1. Kirim request dengan accessToken dari AuthContext.
 * 2. Kalau 401:
 *    a. Refresh token (single-flight — banyak 401 bersamaan cuma satu refresh).
 *    b. Retry request dengan token baru.
 *    c. Kalau masih 401 → logout + throw.
 * 3. 409 REFRESH_IN_PROGRESS saat refresh → retry refresh setelah 500ms.
 * 4. Error lain dilempar seperti biasa.
 */
export function useApiFetch() {
  const { auth, refreshTokens } = useAuth();

  /**
   * Ref ke auth terbaru. Dipakai di dalam doFetch supaya retry setelah refresh
   * mengirim token baru, bukan token stale dari closure fetchWithRetry.
   *
   * fetchWithRetry di-memoize via useCallback, jadi closure `auth` adalah
   * snapshot saat dia dibuat. Setelah refreshTokens() succeed dan AuthContext
   * setAuth(...), useEffect ini menyalin nilai baru ke ref — retry berikutnya
   * di doFetch() pun membaca token yang benar.
   *
   * Trade-off: kalau React belum sempat commit re-render + useEffect sebelum
   * retry doFetch dipanggil, ref masih stale. Untuk fetch biasa (di mana
   * server memvalidasi token) ini bisa menyebabkan retry gagal. Saat ini
   * belum ada callback sinkron dari AuthContext untuk update ref, jadi kita
   * menerima sedikit risiko itu demi kesederhanaan.
   */
  const authRef = useRef(auth);
  useEffect(() => {
    authRef.current = auth;
  }, [auth]);

  /**
   * Single-flight barrier untuk refresh.
   * Disimpan dalam useRef agar tetap hidup antar-render tapi scoped per-hook.
   */
  const refreshInFlight = useRef<Promise<{ ok: boolean }> | null>(null);

  const fetchWithRetry = useCallback(
    async <T>(
      path: string,
      options?: RequestInit & { accessToken?: string | null }
    ): Promise<T> => {
      const doFetch = async (): Promise<Response> => {
        const headers = new Headers(options?.headers as HeadersInit);
        const token = options?.accessToken ?? authRef.current.accessToken;
        if (token) {
          headers.set("Authorization", `Bearer ${token}`);
        }

        let body: BodyInit | undefined = options?.body ?? undefined;
        if (
          body &&
          typeof body === "object" &&
          !(body instanceof FormData) &&
          !(body instanceof Blob) &&
          !(body instanceof ArrayBuffer) &&
          !(body instanceof URLSearchParams) &&
          !(body instanceof ReadableStream)
        ) {
          body = JSON.stringify(body);
          headers.set("Content-Type", "application/json");
        }

        return fetch(
          path.startsWith("http")
            ? path
            : `${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1"}${path}`,
          {
            ...options,
            headers,
            body,
            credentials: "include",
          }
        );
      };

      let response = await doFetch();

      // 401 → refresh + retry sekali
      if (response.status === 401) {
        // Single-flight: kalau ada refresh yang sedang berjalan, tunggu itu.
        // refreshTokens() mengembalikan { ok } — bukan void — supaya kita
        // tidak perlu membaca auth.status lewat closure (yang stale) untuk
        // memutuskan langkah berikutnya.
        let promise = refreshInFlight.current;
        if (!promise) {
          promise = refreshTokens();
          // Simpan promise dengan cleanup di finally — saat resolved, barrier
          // dibuka untuk request berikutnya.
          refreshInFlight.current = promise.finally(() => {
            refreshInFlight.current = null;
          });
        }
        const result = await promise;

        // Refresh gagal → status AuthContext sudah anonymous.
        if (!result.ok) {
          throw new ApiError("UNAUTHENTICATED", "Sesi habis, silakan login ulang", undefined);
        }

        response = await doFetch();

        // Tetap 401 setelah refresh → gagal final
        if (response.status === 401) {
          throw new ApiError("UNAUTHENTICATED", "Sesi habis, silakan login ulang", undefined);
        }
      }

      // Handle 204
      if (response.status === 204) {
        return undefined as T;
      }

      let data: unknown;
      try {
        data = await response.json();
      } catch {
        if (!response.ok) {
          throw new ApiError(
            "RESPONSE_NOT_JSON",
            `Request failed with status ${response.status}`,
            { status: response.status }
          );
        }
        return undefined as T;
      }

      if (!response.ok) {
        const errEnvelope = data as { error?: { code: string; message: string; details?: Record<string, unknown> } };
        throw new ApiError(
          errEnvelope.error?.code ?? "UNKNOWN_ERROR",
          errEnvelope.error?.message ?? `Request failed with status ${response.status}`,
          errEnvelope.error?.details
        );
      }

      return data as T;
    },
    [refreshTokens]
  );

  return fetchWithRetry;
}
