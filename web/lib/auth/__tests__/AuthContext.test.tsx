import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { AuthProvider, useAuth } from "@/lib/auth/AuthContext";
import { setupServer } from "msw/node";

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

// ─── Tests ───────────────────────────────────────────────────────────────────

describe("AuthProvider bootstrap", () => {
  it("bootstrap sukses: /me mengembalikan user → status authenticated", async () => {
    server.use(
      http.get(`${BASE_URL}/auth/me`, () =>
        HttpResponse.json({
          user: {
            id: "u1",
            email: "dosen@example.com",
            full_name: "Dr. Dosen",
            role: "lecturer",
            is_active: true,
            created_at: new Date().toISOString(),
          },
        })
      )
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

  it("bootstrap gagal: /me 401 → status anonymous (BUKAN error)", async () => {
    // Tidak ada handler untuk /auth/me → MSW mengembalikan 404, tapi yang penting
    // AuthProvider tidak throw dan tidak console.error.
    // Untuk test ini kita pastikan 401 eksplisit.
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
        <AuthReporter />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    });
  });
});
