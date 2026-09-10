import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { AuthProvider } from "@/lib/auth/AuthContext";
import { setupServer } from "msw/node";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => "/akun",
  notFound: vi.fn(),
}));

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function ProtectedContent() {
  return <span data-testid="content">konten terproteksi</span>;
}

describe("RequireAuth", () => {
  it('status "loading": tampilkan Memuat, ProtectedContent tidak tampil', async () => {
    // /auth/me tidak pernah resolve -> auth.status = loading selamanya
    server.use(
      http.get(`${BASE_URL}/auth/me`, () => new Promise(() => {}))
    );

    render(
      <AuthProvider>
        <RequireAuth>
          <ProtectedContent />
        </RequireAuth>
      </AuthProvider>
    );

    // RequireAuth render "Memuat..." saat loading
    expect(screen.getByText("Memuat…")).toBeInTheDocument();
    // ProtectedContent tidak boleh tampil
    expect(screen.queryByTestId("content")).not.toBeInTheDocument();
  });

  it('status "anonymous": ProtectedContent tidak tampil (redirect ke /login)', async () => {
    server.use(
      http.get(`${BASE_URL}/auth/me`, () =>
        HttpResponse.json(
          { error: { code: "UNAUTHORIZED", message: "" } },
          { status: 401 }
        )
      )
    );

    render(
      <AuthProvider>
        <RequireAuth>
          <ProtectedContent />
        </RequireAuth>
      </AuthProvider>
    );

    await waitFor(() => {
      // RequireAuth redirect ke /login dan tampilkan "Memuat..." (anonymous = loading)
      expect(screen.getByText("Memuat…")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("content")).not.toBeInTheDocument();
  });

  it('status "authenticated": ProtectedContent tampil', async () => {
    server.use(
      http.get(`${BASE_URL}/auth/me`, () =>
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
      )
    );

    render(
      <AuthProvider>
        <RequireAuth>
          <ProtectedContent />
        </RequireAuth>
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.queryByText("Memuat…")).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("content")).toBeInTheDocument();
  });
});
