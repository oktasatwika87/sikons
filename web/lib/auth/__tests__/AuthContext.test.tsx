import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import { setupServer } from "msw/node";
import { sessionHandlers, anonymousHandlers } from "@/lib/msw/session";

export const BASE_URL = "http://localhost:8080/api/v1";
export const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/** Komponen kecil yang baca auth state dan tampilkan nilainya. */
function AuthReporter() {
  const { auth } = useAuth();
  if (auth.status === "loading") return <span data-testid="status">loading</span>;
  if (auth.status === "anonymous") return <span data-testid="status">anonymous</span>;
  return (
    <span data-testid="status">authenticated</span>
  );
}

// Tests

describe("AuthProvider bootstrap", () => {
  it("bootstrap sukses: refresh lalu /me -> status authenticated", async () => {
    server.use(
      ...sessionHandlers({
        user: {
          id: "u1",
          email: "dosen@example.com",
          full_name: "Dr. Dosen",
          role: "lecturer",
          is_active: true,
          created_at: new Date().toISOString(),
        },
      })
    );

    render(
      <AuthProvider>
        <AuthReporter />
      </AuthProvider>
    );

    // Tahap 1: loading
    expect(screen.getByTestId("status")).toHaveTextContent("loading");

    // Tahap 2: authenticated
    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("authenticated");
    });
  });

  it("bootstrap gagal: refresh 401 (belum login) -> status anonymous (BUKAN error)", async () => {
    server.use(
      ...anonymousHandlers()
    );

    render(
      <AuthProvider>
        <AuthReporter />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    });
  });
});

describe("AuthProvider kontrak endpoint", () => {
  it("bootstrap memakai access token hasil refresh untuk GET /me", async () => {
    let authHeader: string | null = null;
    server.use(
      http.post(`${BASE_URL}/auth/refresh`, () =>
        HttpResponse.json({
          access_token: "token-dari-refresh",
          expires_at: new Date(Date.now() + 15 * 60 * 1000).toISOString(),
        })
      ),
      http.get(`${BASE_URL}/me`, ({ request }) => {
        authHeader = request.headers.get("Authorization");
        return HttpResponse.json({
          user: {
            id: "u1",
            email: "mhs@example.com",
            full_name: "Mahasiswa",
            role: "student",
            is_active: true,
            created_at: new Date().toISOString(),
          },
        });
      })
    );

    render(
      <AuthProvider>
        <AuthReporter />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("authenticated");
    });
    expect(authHeader).toBe("Bearer token-dari-refresh");
  });

  it("logout memakai POST /auth/logout, sesuai rute backend", async () => {
    const logoutMethods: string[] = [];
    server.use(
      ...sessionHandlers({
        user: {
          id: "u1",
          email: "mhs@example.com",
          full_name: "Mahasiswa",
          role: "student",
          is_active: true,
          created_at: new Date().toISOString(),
        },
      }),
      http.all(`${BASE_URL}/auth/logout`, ({ request }) => {
        logoutMethods.push(request.method);
        if (request.method !== "POST") {
          return new HttpResponse(null, { status: 405 });
        }
        return new HttpResponse(null, { status: 204 });
      })
    );

    function LogoutButton() {
      const { logout } = useAuth();
      return (
        <button type="button" onClick={() => void logout()}>
          Keluar
        </button>
      );
    }

    const user = userEvent.setup();
    render(
      <AuthProvider>
        <AuthReporter />
        <LogoutButton />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("authenticated");
    });
    await user.click(screen.getByRole("button", { name: "Keluar" }));

    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    });
    expect(logoutMethods).toEqual(["POST"]);
  });
});
