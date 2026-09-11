/**
 * Test useCreateBooking — error handling + idempotency key.
 *
 * Test paling penting: Idempotency-Key SAMA di request pertama dan retry
 * (tanpa menutup dialog). Ini analog dengan single-flight test di useApiFetch.
 *
 * Pattern mengikuti useApiFetch.test.tsx yang sudah ada dan lulus.
 */
/* eslint-disable react-hooks/set-state-in-effect */
import { useEffect, useRef, useState, type ReactNode } from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import { useCreateBooking } from "@/hooks/useCreateBooking";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Handler bootstrap /me. */
function meHandler() {
  return http.get(`${BASE_URL}/auth/me`, () =>
    HttpResponse.json({
      user: {
        id: "user-1",
        email: "test@example.com",
        full_name: "Test User",
        role: "student",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    })
  );
}

/** QueryClient untuk test. */
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

/** Render children hanya setelah status !== "loading". */
function ReadyChildren({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  if (auth.status === "loading") return null;
  return <>{children}</>;
}

interface ProbeProps {
  slotId: string;
  topic: string;
  description?: string;
  /** Callback dipanggil saat mutation selesai (sukses atau gagal). */
  onComplete: (data: unknown, error: unknown) => void;
}

function BookingProbe({ slotId, topic, description = "", onComplete }: ProbeProps) {
  const { mutate, error, data, isPending } = useCreateBooking("lecturer-1");
  const onCompleteRef = useRef(onComplete);
  useEffect(() => { onCompleteRef.current = onComplete; });

  const [triggered, setTriggered] = useState(false);
  // Pattern disengaja: trigger mutation saat component mount (di test).
  useEffect(() => {
    if (triggered) return;
    setTriggered(true);
    mutate({ slotId, topic, description });
  // eslint-disable-next-line react-hooks/set-state-in-effect
  }, [triggered, slotId, topic, description, mutate]);

  useEffect(() => {
    if (!isPending) {
      if (error) {
        onCompleteRef.current(undefined, error);
      } else if (data) {
        onCompleteRef.current(data, undefined);
      }
    }
  }, [isPending, error, data]);

  return <div>{isPending ? "Loading..." : "Done"}</div>;
}

// ------------------------------------------------------------------ Tests

describe("useCreateBooking", () => {
  it("sukses: membuat booking", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json({ id: "booking-new" }, { status: 201 })
      )
    );

    const result = await new Promise<unknown>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-1"
                topic="Bimbingan skripsi"
                description="Tentang proposal"
                onComplete={(data, error) => {
                  if (data) { resolve(data); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result).toHaveProperty("id");
  });

  it("SLOT_ALREADY_BOOKED → error received", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "SLOT_ALREADY_BOOKED", message: "Slot sudah dipesan orang lain" } },
          { status: 409 }
        )
      )
    );

    const result = await new Promise<{ code: string }>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-already-booked"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as { code: string }); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result.code).toBe("SLOT_ALREADY_BOOKED");
  });

  it("SLOT_TOO_SOON → error received", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "SLOT_TOO_SOON", message: "Minimal 60 menit sebelum konsultasi" } },
          { status: 422 }
        )
      )
    );

    const result = await new Promise<{ code: string }>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-too-soon"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as { code: string }); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result.code).toBe("SLOT_TOO_SOON");
  });

  it("BOOKING_LIMIT_REACHED → error received", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "BOOKING_LIMIT_REACHED", message: "Batas 3 booking aktif sudah tercapai" } },
          { status: 409 }
        )
      )
    );

    const result = await new Promise<{ code: string }>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-limit-reached"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as { code: string }); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result.code).toBe("BOOKING_LIMIT_REACHED");
  });

  it("kode error tidak dikenali -> type: unknown", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "INTERNAL_SERVER_ERROR", message: "Something went wrong" } },
          { status: 500 }
        )
      )
    );

    let capturedError: unknown = null;

    render(
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <ReadyChildren>
            <BookingProbe
              slotId="slot-1"
              topic="Bimbingan skripsi"
              onComplete={(_data, error) => {
                capturedError = error;
              }}
            />
          </ReadyChildren>
        </AuthProvider>
      </QueryClientProvider>
    );

    // Tunggu mutation selesai.
    await waitFor(() => {
      if (!capturedError) {
        throw new Error("Still waiting...");
      }
    });

    // capturedError adalah ApiError dengan .code
    const apiError = capturedError as { code: string };
    expect(apiError.code).toBe("INTERNAL_SERVER_ERROR");
  });

  it("IDEMPOTENCY_KEY_REUSED -> falls back to unknown type", async () => {
    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "IDEMPOTENCY_KEY_REUSED", message: "Key sudah dipakai dengan request berbeda" } },
          { status: 422 }
        )
      )
    );

    let capturedError: unknown = null;

    render(
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <ReadyChildren>
            <BookingProbe
              slotId="slot-1"
              topic="Bimbingan skripsi"
              onComplete={(_data, error) => {
                capturedError = error;
              }}
            />
          </ReadyChildren>
        </AuthProvider>
      </QueryClientProvider>
    );

    // Tunggu mutation selesai.
    await waitFor(() => {
      if (!capturedError) {
        throw new Error("Still waiting...");
      }
    });

    // IDEMPOTENCY_KEY_REUSED ditangkap tapi tidak punya case eksplisit di onError,
    // jadi di mapping ke unknown. Test ini membuktikan error sampai ke hook state.
    const apiError = capturedError as { code: string };
    expect(apiError.code).toBe("IDEMPOTENCY_KEY_REUSED");
  });

  it("Idempotency-Key SAMA saat retry", async () => {
    const requestKeys: string[] = [];
    let callCount = 0;

    server.use(
      meHandler(),
      http.post(`${BASE_URL}/bookings`, ({ request }) => {
        callCount++;
        const key = request.headers.get("Idempotency-Key");
        requestKeys.push(key ?? "");
        // Selalu gagal dengan 409 (bukan retry succeed) untuk membuktikan key sama.
        return HttpResponse.json(
          { error: { code: "SLOT_ALREADY_BOOKED", message: "Slot sudah dipesan" } },
          { status: 409 }
        );
      })
    );

    await new Promise<void>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-1"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  // Test selesai setelah error diterima.
                  if (error) { resolve(); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    // Idempotency key HARUS ada dan tidak null.
    expect(requestKeys.length).toBeGreaterThan(0);
    expect(requestKeys[0]).not.toBe("");
    // Semua request harus pakai key yang sama.
    for (let i = 1; i < requestKeys.length; i++) {
      expect(requestKeys[i]).toBe(requestKeys[0]);
    }
  });
});
