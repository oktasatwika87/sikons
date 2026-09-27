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
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import { useCreateBooking, type CreateBookingError } from "@/hooks/useCreateBooking";
import { sessionHandlers } from "@/lib/msw/session";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Handler bootstrap /me. */
function meHandler() {
  return sessionHandlers({
    user: {
      id: "user-1",
      email: "test@example.com",
      full_name: "Test User",
      role: "student",
      is_active: true,
      created_at: new Date().toISOString(),
    },
  });
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
      ...meHandler(),
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
      ...meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "SLOT_ALREADY_BOOKED", message: "Slot sudah dipesan orang lain" } },
          { status: 409 }
        )
      )
    );

    const result = await new Promise<CreateBookingError>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-already-booked"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as CreateBookingError); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result).toEqual({ type: "already_booked", message: "Slot sudah dipesan orang lain" });
  });

  it("SLOT_TOO_SOON → error received", async () => {
    server.use(
      ...meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "SLOT_TOO_SOON", message: "Minimal 60 menit sebelum konsultasi" } },
          { status: 422 }
        )
      )
    );

    const result = await new Promise<CreateBookingError>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-too-soon"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as CreateBookingError); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result).toEqual({ type: "lead_time", message: "Minimal 60 menit sebelum konsultasi" });
  });

  it("BOOKING_LIMIT_REACHED → error received", async () => {
    server.use(
      ...meHandler(),
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          { error: { code: "BOOKING_LIMIT_REACHED", message: "Batas 3 booking aktif sudah tercapai" } },
          { status: 409 }
        )
      )
    );

    const result = await new Promise<CreateBookingError>((resolve) => {
      render(
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <ReadyChildren>
              <BookingProbe
                slotId="slot-limit-reached"
                topic="Bimbingan skripsi"
                onComplete={(data, error) => {
                  if (error) { resolve(error as CreateBookingError); }
                }}
              />
            </ReadyChildren>
          </AuthProvider>
        </QueryClientProvider>
      );
    });

    expect(result).toEqual({ type: "limit", message: "Batas 3 booking aktif sudah tercapai" });
  });

  it("kode error tidak dikenali -> type: unknown", async () => {
    server.use(
      ...meHandler(),
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

    expect(capturedError).toEqual({ type: "unknown", message: "Something went wrong" });
  });

  it("IDEMPOTENCY_KEY_REUSED -> falls back to unknown type", async () => {
    server.use(
      ...meHandler(),
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
    expect(capturedError).toEqual({
      type: "unknown",
      message: "Key sudah dipakai dengan request berbeda",
    });
  });

  it("Idempotency-Key SAMA saat submit diulang dari dialog yang sama", async () => {
    // Versi lama cuma mengirim satu request, jadi loop pembanding key tidak
    // pernah jalan dan test selalu lolos. Sekarang submit dua kali lewat
    // instance hook yang sama, persis seperti pengguna menekan "Pesan" lagi.
    const requestKeys: string[] = [];

    server.use(
      ...meHandler(),
      http.post(`${BASE_URL}/bookings`, ({ request }) => {
        requestKeys.push(request.headers.get("Idempotency-Key") ?? "");
        return HttpResponse.json(
          { error: { code: "INTERNAL_SERVER_ERROR", message: "Gangguan sementara" } },
          { status: 500 }
        );
      })
    );

    function SubmitTwiceProbe() {
      const { mutate, isPending } = useCreateBooking("lecturer-1");
      return (
        <button
          type="button"
          disabled={isPending}
          onClick={() =>
            mutate({ slotId: "slot-1", topic: "Bimbingan skripsi", description: "" })
          }
        >
          Kirim
        </button>
      );
    }

    render(
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <ReadyChildren>
            <SubmitTwiceProbe />
          </ReadyChildren>
        </AuthProvider>
      </QueryClientProvider>
    );

    const tombol = await screen.findByRole("button", { name: "Kirim" });
    tombol.click();
    await waitFor(() => expect(requestKeys).toHaveLength(1));
    await waitFor(() => expect(tombol).toBeEnabled());
    tombol.click();
    await waitFor(() => expect(requestKeys).toHaveLength(2));

    expect(requestKeys[0]).not.toBe("");
    expect(requestKeys[1]).toBe(requestKeys[0]);
  });
});
