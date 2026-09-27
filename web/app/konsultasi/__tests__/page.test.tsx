/**
 * Test untuk halaman /konsultasi — membuktikan percabangan per role.
 *
 * Dengan data mock yang SAMA, verifikasi:
 * - Mahasiswa: melihat nama dosen + tombol Batalkan
 * - Dosen: melihat nama+mahasiswa + catatan dosen (untuk completed)
 */
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";
import KonsultasiPage from "@/app/konsultasi/page";
import { setupServer } from "msw/node";
import { sessionHandlers } from "@/lib/msw/session";
import {
  bookingHandlers,
  setCurrentRole,
  setBookings,
  resetBookingStore,
} from "@/lib/msw/handlers/booking";

// Mock next/navigation SEBELUM import component
const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  refresh: vi.fn(),
};

vi.mock("next/navigation", () => ({
  useRouter: () => mockRouter,
  usePathname: () => "/konsultasi",
}));

export const BASE_URL = "http://localhost:8080/api/v1";
// Default handler booking menyalin kontrak backend yang asli:
// untuk role selain dosen, lecturer_note dihapus dari respons.
export const server = setupServer(...bookingHandlers);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  resetBookingStore();
  vi.clearAllMocks();
});
afterAll(() => server.close());

// useCancelBooking TIDAK di-mock: PATCH /bookings/:id/cancel dilayani MSW
// supaya jalur error dari hook sampai ke dialog ikut teruji.

// ─── Shared mock data — sama untuk kedua role ──────────────────────────────────

const MOCK_BOOKINGS = [
  {
    id: "booking-1",
    status: "confirmed",
    topic: "Bimbingan Skripsi",
    description: "Diskusi progress skripsi",
    created_at: new Date().toISOString(),
    lecturer_note: null,
    slot: {
      id: "slot-1",
      start_at: new Date(Date.now() + 2 * 24 * 60 * 60 * 1000).toISOString(),
      end_at: new Date(Date.now() + 2 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    },
    lecturer: {
      id: "lecturer-1",
      full_name: "Dr. Ahmad Wijaya",
      department: "Informatika",
    },
    student: {
      id: "student-1",
      full_name: "Budi Santoso",
      identity_number: "2206123456",
    },
  },
  {
    id: "booking-2",
    status: "completed",
    topic: "Konsultasi Proposal",
    description: "Review proposal TA",
    created_at: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString(),
    lecturer_note: "Mahasiswa perlu memperbaiki latar belakang",
    slot: {
      id: "slot-2",
      start_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
      end_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    },
    lecturer: {
      id: "lecturer-1",
      full_name: "Dr. Ahmad Wijaya",
      department: "Informatika",
    },
    student: {
      id: "student-1",
      full_name: "Budi Santoso",
      identity_number: "2206123456",
    },
  },
];

// ─── Test setup helper ─────────────────────────────────────────────────────────

function renderAsRole(role: "student" | "lecturer") {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  const user = role === "student"
    ? {
        id: "student-1",
        email: "budi@ui.ac.id",
        full_name: "Budi Santoso",
        role: "student" as const,
        is_active: true,
        created_at: new Date().toISOString(),
      }
    : {
        id: "lecturer-1",
        email: "ahmad@ui.ac.id",
        full_name: "Dr. Ahmad Wijaya",
        role: "lecturer" as const,
        is_active: true,
        created_at: new Date().toISOString(),
      };

  // Pakai handler default bookingHandlers — setBookings() mengisi data,
  // setCurrentRole() memberitahu handler GET /bookings peran pemanggil
  // supaya lecturer_note hanya dikirim saat role == "lecturer" (sama
  // seperti kontrak backend). Sesi login dipasang via sessionHandlers.
  setCurrentRole(role);
  setBookings(MOCK_BOOKINGS);
  server.use(...sessionHandlers({ user }));

  render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <KonsultasiPage />
      </AuthProvider>
    </QueryClientProvider>
  );
}

// ─── Tests ─────────────────────────────────────────────────────────────────────

describe("Halaman /konsultasi — percabangan per role", () => {
  it("mahasiswa melihat nama DOSEN di daftar", async () => {
    renderAsRole("student");

    await waitFor(() => {
      // Nama dosen harus muncul (2x: mobile card + desktop table)
      expect(screen.getAllByText("Dr. Ahmad Wijaya").length).toBeGreaterThan(0);
    });

    // Tidak boleh lihat NIM mahasiswa sendiri
    expect(screen.queryByText("2206123456")).not.toBeInTheDocument();
  });

  it("mahasiswa MELIHAT tombol Batalkan untuk booking confirmed", async () => {
    renderAsRole("student");

    await waitFor(() => {
      expect(screen.getAllByText("Bimbingan Skripsi").length).toBeGreaterThan(0);
    });

    // Tombol Batalkan harus muncul untuk booking confirmed (ada di mobile atau
    // desktop). Nama persis, karena /batalkan/i juga cocok dengan tombol filter
    // "Dibatalkan" yang selalu ada, jadi assertion lama selalu lolos.
    const batalButtons = screen.getAllByRole("button", { name: "Batalkan" });
    expect(batalButtons.length).toBeGreaterThan(0);
  });

  it("mahasiswa TIDAK melihat catatan dosen (lecturer_note)", async () => {
    renderAsRole("student");

    await waitFor(() => {
      expect(screen.getAllByText("Konsultasi Proposal").length).toBeGreaterThan(0);
    });

    // Catatan dosen tidak muncul untuk mahasiswa
    expect(screen.queryByText(/mahasiswa perlu memperbaiki/i)).not.toBeInTheDocument();
  });

  it("dosen melihat nama+MHS di daftar", async () => {
    renderAsRole("lecturer");

    await waitFor(() => {
      expect(screen.getAllByText("Budi Santoso").length).toBeGreaterThan(0);
    });

    // NIM mahasiswa harus muncul untuk dosen
    expect(screen.getAllByText("2206123456").length).toBeGreaterThan(0);
  });

  it("dosen MELIHAT catatan dosen (lecturer_note) untuk baris completed", async () => {
    renderAsRole("lecturer");

    await waitFor(() => {
      expect(screen.getAllByText("Konsultasi Proposal").length).toBeGreaterThan(0);
    });

    // Catatan dosen HARUS muncul untuk dosen
    expect(screen.getByText(/mahasiswa perlu memperbaiki/i)).toBeInTheDocument();
  });

  it("dosen TIDAK melihat tombol Batalkan", async () => {
    renderAsRole("lecturer");

    await waitFor(() => {
      expect(screen.getAllByText("Bimbingan Skripsi").length).toBeGreaterThan(0);
    });

    // Tombol Batalkan booking tidak boleh muncul untuk dosen. Nama persis
    // supaya tombol filter "Dibatalkan" tidak ikut terhitung.
    expect(screen.queryAllByRole("button", { name: "Batalkan" })).toHaveLength(0);
  });
});

describe("Halaman /konsultasi — pembatalan", () => {
  it("pembatalan yang ditolak backend menampilkan pesannya di dialog", async () => {
    renderAsRole("student");
    server.use(
      http.patch(`${BASE_URL}/bookings/:id/cancel`, () =>
        HttpResponse.json(
          {
            error: {
              code: "CANCEL_TOO_LATE",
              message: "Pembatalan hanya bisa sampai 2 jam sebelum sesi.",
            },
          },
          { status: 409 }
        )
      )
    );
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getAllByText("Bimbingan Skripsi").length).toBeGreaterThan(0);
    });
    // Nama persis "Batalkan": regex /batalkan/i juga kena tombol filter
    // "Dibatalkan".
    await user.click(screen.getAllByRole("button", { name: "Batalkan" })[0]);

    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Batalkan" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Pembatalan hanya bisa sampai 2 jam sebelum sesi."
    );
    // Dialog tetap terbuka supaya pesan bisa dibaca.
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
