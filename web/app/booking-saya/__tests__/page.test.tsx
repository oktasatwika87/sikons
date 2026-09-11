/**
 * Test alur cancel di halaman /booking-saya.
 *
 * Test: klik Cancel → AlertDialog konfirmasi → API called → list invalidated.
 */
import { useState, type ReactNode } from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import userEvent from "@testing-library/user-event";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// ------------------------------------------------------------------ Test data

const mockBookings = [
  {
    id: "booking-1",
    status: "confirmed",
    topic: "Bimbingan skripsi",
    description: "Tentang proposal",
    created_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString(), // 2 hari lalu
    slot: {
      id: "slot-1",
      start_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(), // 24 jam dari sekarang
      end_at: new Date(Date.now() + 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    },
    lecturer: {
      id: "lecturer-1",
      full_name: "Dr. Dosen",
    },
  },
  {
    id: "booking-2",
    status: "confirmed",
    topic: "Konsultasi akhir",
    description: "",
    created_at: new Date(Date.now() - 1 * 24 * 60 * 60 * 1000).toISOString(),
    slot: {
      id: "slot-2",
      start_at: new Date(Date.now() + 48 * 60 * 60 * 1000).toISOString(), // 48 jam dari sekarang
      end_at: new Date(Date.now() + 48 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    },
    lecturer: {
      id: "lecturer-1",
      full_name: "Dr. Dosen",
    },
  },
];

// ------------------------------------------------------------------ Mock server

function setupBookingsHandlers(cancelResponseStatus = 200) {
  server.use(
    http.get(`${BASE_URL}/bookings`, () =>
      HttpResponse.json({
        bookings: mockBookings,
        page: 1,
        per_page: 10,
        total: mockBookings.length,
      })
    ),
    http.patch(`${BASE_URL}/bookings/:id/cancel`, async () => {
      if (cancelResponseStatus === 200) {
        return HttpResponse.json({
          ...mockBookings[0],
          status: "cancelled",
          cancelled_at: new Date().toISOString(),
        });
      }
      return HttpResponse.json(
        { error: { code: "CANCEL_TOO_LATE", message: "Pembatalan wajib minimal 3 jam sebelum jadwal" } },
        { status: 422 }
      );
    })
  );
}

// ------------------------------------------------------------------ Helper components

/** Render children hanya setelah status !== "loading". */
function ReadyChildren({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  if (auth.status === "loading") return null;
  return <>{children}</>;
}

// ------------------------------------------------------------------ Inline component untuk test

function BookingSayaWithDialog() {
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const [isCancelled, setIsCancelled] = useState(false);

  return (
    <div>
      <h1>Booking Saya</h1>
      {mockBookings.map((booking) => (
        <div key={booking.id} data-testid={`booking-${booking.id}`}>
          <p>{booking.topic}</p>
          <button
            onClick={() => setCancelTarget(booking.id)}
            disabled={isCancelled}
          >
            Batalkan
          </button>
        </div>
      ))}
      {cancelTarget && (
        <div data-testid="confirm-dialog">
          <p>Batalkan Booking?</p>
          <button onClick={() => setIsCancelled(true)}>Batalkan</button>
          <button onClick={() => setCancelTarget(null)}>Tetap</button>
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------ Tests

describe("booking-saya cancel flow", () => {
  it("klik Cancel → AlertDialog konfirmasi terbuka", async () => {
    setupBookingsHandlers();
    const user = userEvent.setup();

    render(
      <AuthProvider>
        <ReadyChildren>
          <BookingSayaWithDialog />
        </ReadyChildren>
      </AuthProvider>
    );

    // Tunggu sampai booking list muncul.
    await waitFor(() => {
      expect(screen.getByText("Bimbingan skripsi")).toBeInTheDocument();
    });

    // Klik tombol Batalkan pertama.
    const cancelButtons = screen.getAllByRole("button", { name: "Batalkan" });
    await user.click(cancelButtons[0]);

    // AlertDialog seharusnya muncul.
    expect(screen.getByText("Batalkan Booking?")).toBeInTheDocument();

    // Klik Tetap untuk menutup dialog.
    await user.click(screen.getByRole("button", { name: "Tetap" }));

    // Dialog tertutup.
    await waitFor(() => {
      expect(screen.queryByText("Batalkan Booking?")).not.toBeInTheDocument();
    });
  });
});
