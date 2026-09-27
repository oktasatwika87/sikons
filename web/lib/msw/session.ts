/**
 * Handler MSW untuk bootstrap AuthProvider, mengikuti kontrak backend yang
 * sebenarnya: POST /auth/refresh (cookie) lalu GET /me (Bearer).
 *
 * Dulu tiap test memalsukan GET /auth/me, rute yang tidak ada di backend.
 * Test-nya hijau, tapi di browser sungguhan setiap reload membuat pengguna
 * terlempar ke /login. Semua test yang butuh sesi sebaiknya lewat sini supaya
 * kontraknya cuma ditulis di satu tempat.
 */
import { http, HttpResponse } from "msw";
import type { UserResponse } from "@/lib/api/types";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

/** Sesi aktif: cookie refresh sah, /me membalas user ini. */
export function sessionHandlers(body: { user: UserResponse }) {
  return [
    http.post(`${BASE_URL}/auth/refresh`, () =>
      HttpResponse.json({
        access_token: "access-uji",
        expires_at: new Date(Date.now() + 15 * 60 * 1000).toISOString(),
      })
    ),
    http.get(`${BASE_URL}/me`, () => HttpResponse.json(body)),
  ];
}

/** Belum login: tidak ada cookie refresh, backend membalas 401. */
export function anonymousHandlers() {
  return [
    http.post(`${BASE_URL}/auth/refresh`, () =>
      HttpResponse.json(
        {
          error: {
            code: "REFRESH_TOKEN_INVALID",
            message: "Sesi habis, silakan login ulang",
          },
        },
        { status: 401 }
      )
    ),
  ];
}
