/**
 * Test useApiFetch — retry-once + single-flight + 409 retry + final-fail.
 *
 * Pendekatan: MSW mengintersep HTTP di level fetch global, sehingga hitungan
 * panggilan /auth/refresh cukup disimpan di closure biasa (object `state`),
 * tanpa `vi.hoisted` / `vi.mock`. Mocking di layer yang salah akan menutupi
 * perilaku asli yang justru mau diuji.
 *
 * Prasyarat: AuthProvider sudah selesai bootstrap (status "authenticated")
 * sebelum probe di-render. Tanpa ini, /me dan fetch pertama balapan dan
 * state.auth.status di-closure probe masih "loading".
 */
import { useEffect, useRef, type ReactNode } from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import { useApiFetch } from "@/hooks/useApiFetch";
import { ApiError } from "@/lib/api/errors";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Handler bootstrap /me — AuthProvider boot ke "authenticated" (accessToken null). */
function meHandler() {
  return http.get(`${BASE_URL}/auth/me`, () =>
    HttpResponse.json({
      user: {
        id: "u1",
        email: "test@example.com",
        full_name: "Test User",
        role: "student",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    })
  );
}

/** Render children hanya setelah status !== "loading". */
function ReadyChildren({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  if (auth.status === "loading") return null;
  return <>{children}</>;
}

/** Rekam setiap perubahan status AuthContext — dipakai untuk verifikasi akhir. */
function StatusRecorder({ onStatus }: { onStatus: (s: string) => void }) {
  const { auth } = useAuth();
  // Pakai useEffect supaya ref tidak diakses saat render.
  const seenRef = useRef<string | null>(null);
  useEffect(() => {
    if (seenRef.current !== auth.status) {
      seenRef.current = auth.status;
      onStatus(auth.status);
    }
  }, [auth.status, onStatus]);
  return null;
}

interface FetchProbeProps {
  path: string;
  onResult: (data: unknown) => void;
  onError: (err: unknown) => void;
}

/** Komponen probe — menjalankan satu fetchWithRetry saat mount. */
function FetchProbe({ path, onResult, onError }: FetchProbeProps) {
  const fetchWithRetry = useApiFetch();
  // Simpan callback di ref + update via effect supaya stabil antar-render
  // (callback parent bisa berubah identitas referensinya tiap render).
  const onResultRef = useRef(onResult);
  const onErrorRef = useRef(onError);
  useEffect(() => {
    onResultRef.current = onResult;
    onErrorRef.current = onError;
  });

  useEffect(() => {
    let cancelled = false;
    fetchWithRetry<{ data: string }>(path)
      .then((data) => {
        if (!cancelled) onResultRef.current(data);
      })
      .catch((err) => {
        if (!cancelled) onErrorRef.current(err);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}

// ------------------------------------------------------------------ Tests

describe("useApiFetch", () => {
  it("retry-once sukses: 401 TOKEN_EXPIRED → refresh → retry → 200", async () => {
    const state = { refreshCalls: 0, endpointCalls: 0 };

    server.use(
      meHandler(),
      http.post(`${BASE_URL}/auth/refresh`, () => {
        state.refreshCalls++;
        return HttpResponse.json({
          access_token: "new-access",
          expires_at: new Date(Date.now() + 60_000).toISOString(),
        });
      }),
      http.get(`${BASE_URL}/protected`, () => {
        state.endpointCalls++;
        // Panggilan pertama dapat 401; retry dapat 200.
        if (state.endpointCalls === 1) {
          return HttpResponse.json(
            { error: { code: "TOKEN_EXPIRED", message: "expired" } },
            { status: 401 }
          );
        }
        return HttpResponse.json({ data: "success" });
      })
    );

    const result = await new Promise<unknown>((resolve, reject) => {
      render(
        <AuthProvider>
          <ReadyChildren>
            <FetchProbe
              path="/protected"
              onResult={resolve}
              onError={reject}
            />
          </ReadyChildren>
        </AuthProvider>
      );
    });

    expect(result).toEqual({ data: "success" });
    expect(state.refreshCalls).toBe(1);
    expect(state.endpointCalls).toBe(2);
  });

  it("single-flight: banyak 401 bersamaan → cuma satu /auth/refresh", async () => {
    const state = { refreshCalls: 0, endpointCalls: 0 };
    const PARALLEL = 3;

    server.use(
      meHandler(),
      http.post(`${BASE_URL}/auth/refresh`, async () => {
        state.refreshCalls++;
        // Delay supaya panggilan berikutnya benar-benar menunggu.
        await new Promise((r) => setTimeout(r, 30));
        return HttpResponse.json({
          access_token: "new-access",
          expires_at: new Date(Date.now() + 60_000).toISOString(),
        });
      }),
      http.get(`${BASE_URL}/protected`, () => {
        state.endpointCalls++;
        // Batch pertama: PARALLEL panggilan → semua 401.
        // Batch kedua: PARALLEL retry → semua 200.
        if (state.endpointCalls <= PARALLEL) {
          return HttpResponse.json(
            { error: { code: "TOKEN_EXPIRED", message: "" } },
            { status: 401 }
          );
        }
        return HttpResponse.json({ data: "ok" });
      })
    );

    const all = await new Promise<unknown[]>((resolve, reject) => {
      const settled: unknown[] = [];
      const onSettle = (r: unknown) => {
        settled.push(r);
        if (settled.length === PARALLEL) resolve(settled);
      };
      render(
        <AuthProvider>
          <ReadyChildren>
            <FetchProbe path="/protected" onResult={onSettle} onError={reject} />
            <FetchProbe path="/protected" onResult={onSettle} onError={reject} />
            <FetchProbe path="/protected" onResult={onSettle} onError={reject} />
          </ReadyChildren>
        </AuthProvider>
      );
    });

    expect(all).toHaveLength(PARALLEL);
    for (const r of all) expect(r).toEqual({ data: "ok" });
    // Kunci single-flight: tepat satu refresh call, bukan N.
    expect(state.refreshCalls).toBe(1);
    // Endpoint dipanggil 2x per probe (401 lalu retry) → 2 * PARALLEL.
    expect(state.endpointCalls).toBe(2 * PARALLEL);
  });

  it("409 REFRESH_IN_PROGRESS dari /auth/refresh → retry setelah delay, lalu sukses", async () => {
    const state = { refreshCalls: 0, endpointCalls: 0 };

    server.use(
      meHandler(),
      http.post(`${BASE_URL}/auth/refresh`, () => {
        state.refreshCalls++;
        // Panggilan pertama: simulasi "sedang ada refresh lain" → 409.
        if (state.refreshCalls === 1) {
          return HttpResponse.json(
            { error: { code: "REFRESH_IN_PROGRESS", message: "in progress" } },
            { status: 409 }
          );
        }
        return HttpResponse.json({
          access_token: "new-access",
          expires_at: new Date(Date.now() + 60_000).toISOString(),
        });
      }),
      http.get(`${BASE_URL}/protected`, () => {
        state.endpointCalls++;
        if (state.endpointCalls === 1) {
          return HttpResponse.json(
            { error: { code: "TOKEN_EXPIRED", message: "" } },
            { status: 401 }
          );
        }
        return HttpResponse.json({ data: "ok" });
      })
    );

    const start = Date.now();
    const result = await new Promise<unknown>((resolve, reject) => {
      render(
        <AuthProvider>
          <ReadyChildren>
            <FetchProbe path="/protected" onResult={resolve} onError={reject} />
          </ReadyChildren>
        </AuthProvider>
      );
    });
    const elapsed = Date.now() - start;

    expect(result).toEqual({ data: "ok" });
    // Retry refresh: 2 panggilan /auth/refresh (1 gagal 409, 1 sukses).
    expect(state.refreshCalls).toBe(2);
    // Refresh pertama delay 500ms sebelum retry — minimal harus sudah lewat.
    expect(elapsed).toBeGreaterThanOrEqual(450);
    // AuthContext TIDAK treat 409 sebagai anonymous — request asli tetap resolve.
  });

  it("refresh gagal final (401) → status anonymous + request asli throw UNAUTHENTICATED", async () => {
    const state = { refreshCalls: 0, endpointCalls: 0 };
    const statuses: string[] = [];

    server.use(
      meHandler(),
      http.post(`${BASE_URL}/auth/refresh`, () => {
        state.refreshCalls++;
        return HttpResponse.json(
          {
            error: {
              code: "REFRESH_TOKEN_INVALID",
              message: "refresh token tidak valid",
            },
          },
          { status: 401 }
        );
      }),
      http.get(`${BASE_URL}/protected`, () => {
        state.endpointCalls++;
        return HttpResponse.json(
          { error: { code: "TOKEN_EXPIRED", message: "" } },
          { status: 401 }
        );
      })
    );

    let capturedError: unknown = null;

    render(
      <AuthProvider>
        <ReadyChildren>
          <StatusRecorder onStatus={(s) => statuses.push(s)} />
          <FetchProbe
            path="/protected"
            onResult={() => {
              /* tidak dipanggil — test ini expect error */
            }}
            onError={(e) => {
              capturedError = e;
            }}
          />
        </ReadyChildren>
      </AuthProvider>
    );

    // Tunggu sampai error tertangkap.
    await waitFor(() => {
      expect(capturedError).not.toBeNull();
    });

    expect(capturedError).toBeInstanceOf(ApiError);
    expect((capturedError as ApiError).code).toBe("UNAUTHENTICATED");
    // Refresh hanya dicoba sekali — gagal → setAnonymous(), bukan loop.
    expect(state.refreshCalls).toBe(1);
    // Tidak ada retry endpoint setelah refresh gagal (result.ok === false
    // → throw langsung sebelum doFetch kedua).
    expect(state.endpointCalls).toBe(1);
    // AuthContext sudah anonymous — bukan stuck di "authenticated".
    await waitFor(() => {
      expect(statuses).toContain("anonymous");
    });
  });
});
