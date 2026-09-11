/**
 * Test halaman /ketersediaan — MSW integration tests.
 *
 * Test terpenting:
 * 1. Summary panel muncul dengan bookings_cancelled > 0 + penekanan visual
 * 2. ATURAN_BENTROK -> pesan spesifik
 */
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { userEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";
import KetersediaanPage from "@/app/ketersediaan/page";

// Mock next/navigation SEBELUM import component lain
const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  refresh: vi.fn(),
};
const mockPathname = "/ketersediaan";

vi.mock("next/navigation", () => ({
  useRouter: () => mockRouter,
  usePathname: () => mockPathname,
}));

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Handler bootstrap /me — role lecturer supaya nav tampilkan link ketersediaan. */
function meHandler(overrides?: { id?: string; role?: string }) {
  return http.get(`${BASE_URL}/auth/me`, () =>
    HttpResponse.json({
      user: {
        id: overrides?.id ?? "lecturer-1",
        email: "dosen@example.com",
        full_name: "Dr. Dosen",
        role: overrides?.role ?? "lecturer",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    })
  );
}

/** Rules handler — daftar kosong. */
function emptyRulesHandler() {
  return http.get(`${BASE_URL}/availability-rules`, () =>
    HttpResponse.json([])
  );
}

/** Exceptions handler — daftar kosong. */
function emptyExceptionsHandler() {
  return http.get(`${BASE_URL}/availability-exceptions`, ({ request }) => {
    const url = new URL(request.url);
    return HttpResponse.json([]);
  });
}

/** QueryClient untuk test. */
const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

/** Render halaman dengan provider. */
function renderPage() {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <KetersediaanPage />
      </AuthProvider>
    </QueryClientProvider>
  );
}

describe("/ketersediaan page", () => {
  it("renders rules and exceptions sections", async () => {
    server.use(
      meHandler(),
      emptyRulesHandler(),
      emptyExceptionsHandler()
    );

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Jadwal Rutin")).toBeInTheDocument();
      expect(screen.getByText("Pengecualian")).toBeInTheDocument();
    });
  });

  it("summary panel appears with bookings_cancelled > 0 after creating rule", async () => {
    let callCount = 0;
    server.use(
      meHandler(),
      emptyRulesHandler(),
      emptyExceptionsHandler(),
      http.post(`${BASE_URL}/availability-rules`, () => {
        callCount++;
        return HttpResponse.json(
          {
            id: "new-rule-id",
            slots: {
              created: 6,
              deleted: 0,
              withdrawn: 0,
              restored: 0,
              bookings_cancelled: 2,
            },
          },
          { status: 201 }
        );
      }),
      // After mutation, re-fetch returns the new rule
      http.get(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json([
          {
            id: "new-rule-id",
            lecturer_id: "lecturer-1",
            day_of_week: 1,
            start_time: "09:00",
            end_time: "12:00",
            slot_duration_min: 30,
            effective_from: "2026-09-14",
            is_active: true,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        ])
      )
    );

    renderPage();

    // Klik tombol tambah aturan
    const addButton = await waitFor(() =>
      screen.getByRole("button", { name: "Tambah Aturan" })
    );
    await userEvent.click(addButton);

    // Submit form
    const submitButton = await waitFor(() =>
      screen.getByRole("button", { name: "Simpan" })
    );
    await userEvent.click(submitButton);

    // Summary panel harus muncul dengan bookings_cancelled
    await waitFor(() => {
      expect(
        screen.getByText("Booking dibatalkan", { exact: true })
      ).toBeInTheDocument();
    });

    // Penekanan visual: cari badge dengan text bookings_cancelled
    const badge = await waitFor(() =>
      screen.getByText((content) => content.includes("2") && content.includes("booking dibatalkan"))
    );
    expect(badge).toBeInTheDocument();
  });

  it("summary panel appears with bookings_cancelled > 0 after ending rule", async () => {
    let callCount = 0;
    server.use(
      meHandler(),
      http.get(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json([
          {
            id: "rule-1",
            lecturer_id: "lecturer-1",
            day_of_week: 1,
            start_time: "09:00",
            end_time: "12:00",
            slot_duration_min: 30,
            effective_from: "2026-09-01",
            is_active: true,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        ])
      ),
      emptyExceptionsHandler(),
      http.patch(`${BASE_URL}/availability-rules/rule-1`, () => {
        callCount++;
        return HttpResponse.json({
          slots: {
            created: 0,
            deleted: 3,
            withdrawn: 1,
            restored: 0,
            bookings_cancelled: 1,
          },
        });
      }),
      // After mutation, re-fetch returns updated rule
      http.get(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json([
          {
            id: "rule-1",
            lecturer_id: "lecturer-1",
            day_of_week: 1,
            start_time: "09:00",
            end_time: "12:00",
            slot_duration_min: 30,
            effective_from: "2026-09-01",
            effective_to: new Date().toISOString().slice(0, 10),
            is_active: false,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        ])
      )
    );

    renderPage();

    // Klik tombol "Akhiri" pertama di daftar aturan
    const endButtons = await waitFor(() =>
      screen.getAllByRole("button", { name: "Akhiri" })
    );
    await userEvent.click(endButtons[0]);

    // Konfirmasi di dialog — tombol "Akhiri" di dalam dialog (yang kedua)
    const confirmButtons = await waitFor(() =>
      screen.getAllByRole("button", { name: "Akhiri" })
    );
    await userEvent.click(confirmButtons[1]);

    // Summary panel harus muncul dengan bookings_cancelled
    await waitFor(() => {
      expect(
        screen.getByText("Booking dibatalkan", { exact: true })
      ).toBeInTheDocument();
    });
  });

  it("ATURAN_BENTROK -> shows specific error message", async () => {
    server.use(
      meHandler(),
      emptyRulesHandler(),
      emptyExceptionsHandler(),
      http.post(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json(
          {
            error: {
              code: "ATURAN_BENTROK",
              message: "Sudah ada aturan aktif di hari dan rentang jam yang sama",
            },
          },
          { status: 409 }
        )
      )
    );

    renderPage();

    // Klik tombol tambah aturan
    const addButton = await waitFor(() =>
      screen.getByRole("button", { name: "Tambah Aturan" })
    );
    await userEvent.click(addButton);

    // Submit form (default values already filled)
    const submitButton = await waitFor(() =>
      screen.getByRole("button", { name: "Simpan" })
    );
    await userEvent.click(submitButton);

    // Pesan error spesifik harus muncul
    await waitFor(() => {
      expect(
        screen.getByText((content) =>
          content.includes("Sudah ada aturan aktif di hari dan rentang jam yang sama")
        )
      ).toBeInTheDocument();
    });

    // Tombol submit masih aktif (tidak crash)
    expect(submitButton).toBeEnabled();
  });

  it("shows summary with zero bookings_cancelled without destructive styling", async () => {
    server.use(
      meHandler(),
      emptyRulesHandler(),
      emptyExceptionsHandler(),
      http.post(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json(
          {
            id: "new-rule-id",
            slots: {
              created: 6,
              deleted: 0,
              withdrawn: 0,
              restored: 0,
              bookings_cancelled: 0,
            },
          },
          { status: 201 }
        )
      ),
      http.get(`${BASE_URL}/availability-rules`, () =>
        HttpResponse.json([
          {
            id: "new-rule-id",
            lecturer_id: "lecturer-1",
            day_of_week: 1,
            start_time: "09:00",
            end_time: "12:00",
            slot_duration_min: 30,
            effective_from: "2026-09-14",
            is_active: true,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        ])
      )
    );

    renderPage();

    const addButton = await waitFor(() =>
      screen.getByRole("button", { name: "Tambah Aturan" })
    );
    await userEvent.click(addButton);

    const submitButton = await waitFor(() =>
      screen.getByRole("button", { name: "Simpan" })
    );
    await userEvent.click(submitButton);

    // Summary panel harus muncul tapi tanpa destructive badge
    await waitFor(() => {
      expect(screen.getByText("Jadwal diperbarui")).toBeInTheDocument();
    });

    // Tidak ada destructive badge dengan "booking dibatalkan"
    const destructiveBadge = screen.queryByText((content) =>
      content.includes("booking dibatalkan")
    );
    expect(destructiveBadge).not.toBeInTheDocument();
  });
});
