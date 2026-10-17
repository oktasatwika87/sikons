/**
 * Test LecturerDetail yang asli (bukan salinan di dalam test):
 *  - tombol "Sebelumnya" mati di minggu berjalan, hidup lagi setelah maju,
 *  - request slot memakai rentang Senin 00:00 WIB minggu berjalan,
 *  - 409 SLOT_ALREADY_BOOKED dari POST /bookings memunculkan banner di
 *    halaman setelah dialog tertutup.
 */
import { type ReactNode } from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";
import { LecturerDetail } from "@/components/lecturer/LecturerDetail";
import { formatTimeRange, startOfWeekJakarta } from "@/lib/date";
import { sessionHandlers } from "@/lib/msw/session";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

const JAM_MS = 60 * 60 * 1000;

// Slot di Senin minggu berjalan jam 03:00 WIB, jadi selalu tampil di minggu
// ini hari apa pun test dijalankan.
function slotMingguIni() {
  const start = new Date(startOfWeekJakarta().getTime() + 3 * JAM_MS);
  const end = new Date(start.getTime() + 30 * 60 * 1000);
  return {
    id: "slot-1",
    start_at: start.toISOString(),
    end_at: end.toISOString(),
    status: "open" as const,
  };
}

function setupHandlers(slotRequests: URL[]) {
  server.use(
    ...sessionHandlers({
      user: {
        id: "user-1",
        email: "mhs@example.com",
        full_name: "Mahasiswa Uji",
        role: "student",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    }),
    http.get(`${BASE_URL}/lecturers/:id`, () =>
      HttpResponse.json({
        id: "lecturer-1",
        full_name: "Dr. Dosen",
        department: "Teknik Informatika",
        room: "R.301",
        bio: "Dosen pilihan",
      })
    ),
    http.get(`${BASE_URL}/lecturers/:id/slots`, ({ request }) => {
      const url = new URL(request.url);
      slotRequests.push(url);
      const from = url.searchParams.get("from");
      const slot = slotMingguIni();
      // Slot cuma dikirim kalau rentang yang diminta memang minggu berjalan.
      const slots = from === startOfWeekJakarta().toISOString() ? [slot] : [];
      return HttpResponse.json({
        lecturer_id: "lecturer-1",
        from,
        to: url.searchParams.get("to"),
        slots,
      });
    })
  );
}

function renderDetail(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{ui}</AuthProvider>
    </QueryClientProvider>
  );
}

describe("LecturerDetail: navigasi minggu", () => {
  it("Sebelumnya mati di minggu berjalan dan hidup lagi setelah maju satu minggu", async () => {
    const slotRequests: URL[] = [];
    setupHandlers(slotRequests);
    const user = userEvent.setup();

    renderDetail(<LecturerDetail lecturerId="lecturer-1" />);

    await screen.findByText("Dr. Dosen");
    const prev = screen.getByRole("button", { name: "Sebelumnya" });
    const next = screen.getByRole("button", { name: "Berikutnya" });

    // Tunggu fetch slot pertama selesai supaya isFetching tidak ikut
    // menentukan status tombol.
    await waitFor(() => expect(next).toBeEnabled());
    expect(slotRequests[0].searchParams.get("from")).toBe(
      startOfWeekJakarta().toISOString()
    );
    expect(prev).toBeDisabled();

    await user.click(next);
    await waitFor(() => expect(prev).toBeEnabled());

    await user.click(prev);
    await waitFor(() => expect(next).toBeEnabled());
    expect(prev).toBeDisabled();
  });
});

describe("LecturerDetail: SLOT_ALREADY_BOOKED", () => {
  it("409 dari POST /bookings menutup dialog dan memunculkan banner", async () => {
    const slotRequests: URL[] = [];
    setupHandlers(slotRequests);
    server.use(
      http.post(`${BASE_URL}/bookings`, () =>
        HttpResponse.json(
          {
            error: {
              code: "SLOT_ALREADY_BOOKED",
              message: "Slot ini baru saja dipesan orang lain.",
            },
          },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();

    renderDetail(<LecturerDetail lecturerId="lecturer-1" />);

    const slot = slotMingguIni();
    const label = formatTimeRange(slot.start_at, slot.end_at);
    // Slot dirender dua kali (grid desktop + daftar mobile), ambil salah satu.
    const slotButtons = await screen.findAllByRole("button", { name: label });
    await user.click(slotButtons[0]);

    await user.type(screen.getByLabelText(/Topik/), "Bimbingan skripsi");
    await user.click(screen.getByRole("button", { name: "Pesan" }));

    const banner = await screen.findByRole("alert");
    expect(banner).toHaveTextContent("Slot ini baru saja dipesan orang lain.");
    expect(screen.queryByRole("button", { name: "Pesan" })).not.toBeInTheDocument();
  });
});
