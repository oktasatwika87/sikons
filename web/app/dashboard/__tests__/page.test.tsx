/**
 * Test halaman /dashboard yang asli (sebelumnya test ini merender salinan
 * dashboard buatan sendiri di dalam file test, jadi tidak menguji kode
 * produksi sama sekali).
 *
 * Yang dicek:
 *  - tandai selesai mengirim PATCH berisi catatan, dialog tertutup, dan
 *    booking hilang dari daftar setelah query di-refetch,
 *  - catatan kosong tidak ikut dikirim,
 *  - error dari PATCH no-show tampil di dialog, bukan hilang diam-diam.
 */
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { sessionHandlers } from "@/lib/msw/session";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }),
  usePathname: () => "/dashboard",
}));

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// Impor SETELAH vi.mock supaya RequireAuth dapat router palsu.
const { default: DashboardPage } = await import("@/app/dashboard/page");
const { AuthProvider } = await import("@/lib/auth/AuthContext");

const JAM = 60 * 60 * 1000;

function buatBooking(id: string, topic: string, mulaiDariSekarangMs: number) {
  const start = new Date(Date.now() + mulaiDariSekarangMs);
  return {
    id,
    status: "confirmed",
    topic,
    description: "",
    created_at: new Date(Date.now() - 48 * JAM).toISOString(),
    lecturer_note: null,
    slot: {
      id: `slot-${id}`,
      start_at: start.toISOString(),
      end_at: new Date(start.getTime() + 30 * 60 * 1000).toISOString(),
    },
    student: { id: `mhs-${id}`, full_name: "Budi Santoso", identity_number: "2206123456" },
  };
}

interface Rekaman {
  completeBodies: Record<string, unknown>[];
}

function setupHandlers(): Rekaman {
  const rekaman: Rekaman = { completeBodies: [] };
  // Booking yang sudah lewat 1 jam: masuk bucket "Perlu Ditindaklanjuti".
  let confirmed = [buatBooking("b1", "Bimbingan skripsi", -JAM)];

  server.use(
    ...sessionHandlers({
      user: {
        id: "dosen-1",
        email: "dosen@example.com",
        full_name: "Dr. Dosen",
        role: "lecturer",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    }),
    http.get(`${BASE_URL}/bookings`, () =>
      HttpResponse.json({
        bookings: confirmed,
        page: 1,
        per_page: 100,
        total: confirmed.length,
      })
    ),
    http.patch(`${BASE_URL}/bookings/:id/complete`, async ({ params, request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      rekaman.completeBodies.push(body);
      const booking = confirmed.find((b) => b.id === params.id)!;
      confirmed = confirmed.filter((b) => b.id !== params.id);
      return HttpResponse.json({
        ...booking,
        status: "completed",
        lecturer_note: (body.lecturer_note as string | undefined) ?? null,
      });
    }),
    http.patch(`${BASE_URL}/bookings/:id/no-show`, () =>
      HttpResponse.json(
        {
          error: {
            code: "BOOKING_ALREADY_FINALIZED",
            message: "Booking ini sudah difinalisasi.",
          },
        },
        { status: 409 }
      )
    )
  );
  return rekaman;
}

function renderDashboard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <DashboardPage />
      </AuthProvider>
    </QueryClientProvider>
  );
}

describe("/dashboard: tandai selesai", () => {
  it("mengirim catatan, menutup dialog, dan booking hilang dari daftar", async () => {
    const rekaman = setupHandlers();
    const user = userEvent.setup();
    renderDashboard();

    await screen.findByText("Bimbingan skripsi");
    await user.click(screen.getByRole("button", { name: "Tandai Selesai" }));

    const dialog = await screen.findByRole("dialog");
    await user.type(
      within(dialog).getByLabelText("Catatan (opsional)"),
      "Perlu revisi bab 2"
    );
    expect(within(dialog).getByText("18/2000 karakter")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Simpan" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(rekaman.completeBodies).toEqual([{ lecturer_note: "Perlu revisi bab 2" }]);
    await waitFor(() =>
      expect(screen.queryByText("Bimbingan skripsi")).not.toBeInTheDocument()
    );
  });

  it("catatan kosong tidak ikut dikirim", async () => {
    const rekaman = setupHandlers();
    const user = userEvent.setup();
    renderDashboard();

    await screen.findByText("Bimbingan skripsi");
    await user.click(screen.getByRole("button", { name: "Tandai Selesai" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Simpan" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(rekaman.completeBodies).toEqual([{}]);
  });
});

describe("/dashboard: tidak hadir", () => {
  it("penolakan dari backend tampil di dialog", async () => {
    setupHandlers();
    const user = userEvent.setup();
    renderDashboard();

    await screen.findByText("Bimbingan skripsi");
    await user.click(screen.getByRole("button", { name: "Tidak Hadir" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Ya, Tidak Hadir" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Booking ini sudah difinalisasi."
    );
  });
});
