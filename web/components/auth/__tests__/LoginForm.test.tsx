/**
 * Test LoginForm — fokus: redirect setelah login sukses.
 *
 * - Tanpa ?next= → fallback ke "/".
 * - Dengan ?next=/akun → push ke "/akun".
 *
 * next/navigation di-mock dengan vi.mock karena next/router bukan layer HTTP —
 * beda dengan useApiFetch yang sepenuhnya MSW.
 */
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

const mockRouter = { push: vi.fn(), replace: vi.fn(), refresh: vi.fn() };
let mockSearchParams = new URLSearchParams("");

vi.mock("next/navigation", () => ({
  useRouter: () => mockRouter,
  useSearchParams: () => mockSearchParams,
  usePathname: () => "/login",
}));

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  mockRouter.push.mockReset();
  mockRouter.replace.mockReset();
  mockRouter.refresh.mockReset();
  mockSearchParams = new URLSearchParams("");
});
afterAll(() => server.close());

// Impor SETELAH vi.mock — agar hook dapat versi yang sudah di-mock.
const { LoginForm } = await import("@/components/auth/LoginForm");
const { AuthProvider } = await import("@/lib/auth/AuthContext");

/** Setup MSW: /auth/me 401 (belum login) + /auth/login 200 dengan data user. */
function setupLoginHandlers() {
  server.use(
    http.get(`${BASE_URL}/auth/me`, () =>
      HttpResponse.json(
        { error: { code: "UNAUTHORIZED", message: "" } },
        { status: 401 }
      )
    ),
    http.post(`${BASE_URL}/auth/login`, () =>
      HttpResponse.json({
        user: {
          id: "u1",
          email: "mahasiswa@uni.ac.id",
          full_name: "Mahasiswa Test",
          role: "student",
          is_active: true,
          created_at: new Date().toISOString(),
        },
        access_token: "test-access-token",
        expires_at: new Date(Date.now() + 60_000).toISOString(),
        refresh_expires_at: new Date(Date.now() + 7 * 86_400_000).toISOString(),
      })
    )
  );
}

describe("LoginForm redirect setelah login sukses", () => {
  it("tanpa ?next= → redirect ke /", async () => {
    mockSearchParams = new URLSearchParams("");
    setupLoginHandlers();

    render(
      <AuthProvider>
        <LoginForm />
      </AuthProvider>
    );

    // Tunggu bootstrap selesai — AuthProvider harus anonymous dulu.
    await waitFor(() => {
      expect(screen.queryByText("Memuat…")).not.toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "mahasiswa@uni.ac.id" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Masuk" }));

    await waitFor(() => {
      expect(mockRouter.push).toHaveBeenCalledWith("/");
    });
    // router.refresh() dipanggil supaya server component render ulang dengan
    // cookie baru.
    expect(mockRouter.refresh).toHaveBeenCalled();
  });

  it("dengan ?next=/akun → redirect ke /akun (bukan /)", async () => {
    mockSearchParams = new URLSearchParams("next=%2Fakun");
    setupLoginHandlers();

    render(
      <AuthProvider>
        <LoginForm />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.queryByText("Memuat…")).not.toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "mahasiswa@uni.ac.id" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Masuk" }));

    await waitFor(() => {
      expect(mockRouter.push).toHaveBeenCalledWith("/akun");
    });
    // Pastiin TIDAK push ke fallback "/" ketika ?next= ada.
    expect(mockRouter.push).not.toHaveBeenCalledWith("/");
    expect(mockRouter.refresh).toHaveBeenCalled();
  });

  it("dengan ?next= kosong/absurd → fallback ke /", async () => {
    // next= tanpa nilai — searchParams.get("next") kembalikan "" (string kosong),
    // LoginForm treat ini sebagai "tidak ada next" → fallback ke /.
    mockSearchParams = new URLSearchParams("next=");
    setupLoginHandlers();

    render(
      <AuthProvider>
        <LoginForm />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.queryByText("Memuat…")).not.toBeInTheDocument();
    });

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "mahasiswa@uni.ac.id" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Masuk" }));

    await waitFor(() => {
      expect(mockRouter.push).toHaveBeenCalledWith("/");
    });
  });
});
