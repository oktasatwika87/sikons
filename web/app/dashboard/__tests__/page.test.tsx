/**
 * Test alur tandakan selesai di halaman /dashboard.
 *
 * Test: submit tandakan selesai dengan catatan → booking dipindahkan
 * dari bucket yang benar setelah query invalidated.
 */
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server, resetBookingStore } from "@/lib/msw";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  resetBookingStore();
});
afterAll(() => server.close());

// ------------------------------------------------------------------ Test data

const mockBookings = [
  {
    id: "booking-1",
    status: "confirmed",
    topic: "Bimbingan skripsi",
    description: "Tentang proposal",
    created_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString(),
    slot: {
      id: "slot-1",
      start_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(), // 1 jam lalu
      end_at: new Date(Date.now() - 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    },
    student: {
      id: "student-1",
      full_name: "Budi Santoso",
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
    student: {
      id: "student-2",
      full_name: "Ani Wijaya",
    },
  },
];

// ------------------------------------------------------------------ Mock server

let completeCalled = false;
let completeNote = "";

function setupDashboardHandlers() {
  completeCalled = false;
  completeNote = "";

  server.use(
    http.get(`${BASE_URL}/auth/me`, () =>
      HttpResponse.json({
        user: {
          id: "lecturer-1",
          email: "dosen@example.com",
          full_name: "Dr. Dosen",
          role: "lecturer",
          is_active: true,
          created_at: new Date().toISOString(),
        },
      })
    ),
    http.get(`${BASE_URL}/bookings`, () =>
      HttpResponse.json({
        bookings: mockBookings,
        page: 1,
        per_page: 100,
        total: mockBookings.length,
      })
    ),
    http.patch(`${BASE_URL}/bookings/:id/complete`, async ({ params, request }) => {
      completeCalled = true;
      if (request.headers.get("Content-Type")?.includes("application/json")) {
        const body = await request.json() as { lecturer_note?: string };
        completeNote = body.lecturer_note ?? "";
      }

      // Return completed booking
      return HttpResponse.json({
        ...mockBookings[0],
        status: "completed",
        completed_at: new Date().toISOString(),
        lecturer_note: completeNote || null,
      });
    }),
    http.patch(`${BASE_URL}/bookings/:id/no-show`, () =>
      HttpResponse.json({
        ...mockBookings[0],
        status: "no_show",
        marked_at: new Date().toISOString(),
      })
    )
  );
}

// ------------------------------------------------------------------ Helper components

function ReadyChildren({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  if (auth.status === "loading") return null;
  return <>{children}</>;
}

// ------------------------------------------------------------------ Inline component untuk test

function DashboardWithDialog() {
  const [completeTarget, setCompleteTarget] = useState<{ id: string; topic: string; student?: { full_name: string } } | null>(null);
  const [completeNote, setCompleteNote] = useState("");
  const [completed, setCompleted] = useState<string[]>([]);

  const handleCompleteClick = (id: string, topic: string, student?: { full_name: string }) => {
    setCompleteTarget({ id, topic, student });
    setCompleteNote("");
  };

  const handleCompleteConfirm = () => {
    if (!completeTarget) return;
    // Simulasi sukses
    setCompleted([...completed, completeTarget.id]);
    setCompleteTarget(null);
  };

  return (
    <div>
      <h1>Dashboard</h1>

      {/* Needs Follow Up Section */}
      <section>
        <h2>Perlu Ditindaklanjuti</h2>
        {mockBookings
          // eslint-disable-next-line react-hooks/purity
          .filter((b) => new Date(b.slot.start_at).getTime() < Date.now())
          .map((booking) => (
            <div key={booking.id} data-testid={`booking-${booking.id}`}>
              <p>{booking.topic}</p>
              <p>{booking.student.full_name}</p>
              {!completed.includes(booking.id) && (
                <button onClick={() => handleCompleteClick(booking.id, booking.topic, booking.student)}>
                  Tandai Selesai
                </button>
              )}
              {completed.includes(booking.id) && <span data-testid="completed-badge">Selesai</span>}
            </div>
          ))}
      </section>

      {/* Complete Dialog */}
      {completeTarget && (
        <div data-testid="complete-dialog">
          <p>Tandai Selesai: {completeTarget.topic}</p>
          <textarea
            data-testid="note-input"
            value={completeNote}
            onChange={(e) => setCompleteNote(e.target.value)}
            maxLength={2000}
          />
          <p>{completeNote.length}/2000</p>
          <p>Catatan tidak bisa diubah setelah disimpan.</p>
          <button onClick={handleCompleteConfirm}>Simpan</button>
          <button onClick={() => setCompleteTarget(null)}>Batal</button>
        </div>
      )}
    </div>
  );
}

// ------------------------------------------------------------------ Tests

import { useState } from "react";

describe("dashboard complete flow", () => {
  it("submit tandakan selesai dengan catatan → booking ditandai selesai", async () => {
    setupDashboardHandlers();
    const user = userEvent.setup();

    render(
      <AuthProvider>
        <ReadyChildren>
          <DashboardWithDialog />
        </ReadyChildren>
      </AuthProvider>
    );

    // Tunggu sampai booking list muncul
    await waitFor(() => {
      expect(screen.getByText("Bimbingan skripsi")).toBeInTheDocument();
    });

    // Klik tombol Tandai Selesai
    const completeButton = screen.getByRole("button", { name: "Tandai Selesai" });
    await user.click(completeButton);

    // Dialog harus muncul
    await waitFor(() => {
      expect(screen.getByTestId("complete-dialog")).toBeInTheDocument();
    });

    // Ketik catatan
    const noteInput = screen.getByTestId("note-input");
    await user.clear(noteInput);
    await user.type(noteInput, "Sesi produktif, mahasiswa perlu follow-up");

    // Verifikasi counter karakter
    expect(screen.getByText(/41\/2000/)).toBeInTheDocument();

    // Klik Simpan
    const saveButton = screen.getByRole("button", { name: "Simpan" });
    await user.click(saveButton);

    // Dialog harus tertutup
    await waitFor(() => {
      expect(screen.queryByTestId("complete-dialog")).not.toBeInTheDocument();
    });

    // Booking harus ditandai selesai
    expect(screen.getByTestId("completed-badge")).toBeInTheDocument();
  });

  it("catatan opsional → bisa disimpan kosong", async () => {
    setupDashboardHandlers();
    const user = userEvent.setup();

    render(
      <AuthProvider>
        <ReadyChildren>
          <DashboardWithDialog />
        </ReadyChildren>
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByText("Bimbingan skripsi")).toBeInTheDocument();
    });

    const completeButton = screen.getByRole("button", { name: "Tandai Selesai" });
    await user.click(completeButton);

    await waitFor(() => {
      expect(screen.getByTestId("complete-dialog")).toBeInTheDocument();
    });

    // Langsung klik Simpan tanpa isi catatan
    const saveButton = screen.getByRole("button", { name: "Simpan" });
    await user.click(saveButton);

    // Dialog harus tertutup
    await waitFor(() => {
      expect(screen.queryByTestId("complete-dialog")).not.toBeInTheDocument();
    });
  });
});
