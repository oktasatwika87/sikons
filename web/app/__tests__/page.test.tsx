import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { AuthProvider } from "@/lib/auth/AuthContext";
import { RoleRedirect } from "@/components/auth/RoleRedirect";
import { setupServer } from "msw/node";
import { useRouter } from "next/navigation";

export const BASE_URL = "http://localhost:8080/api/v1";
export const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

// ─── Mock router ───────────────────────────────────────────────────────────────

const mockReplace = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({
    replace: mockReplace,
  }),
  usePathname: () => "/",
}));

// ─── Helper: render RoleRedirect dengan user tertentu ──────────────────────────

async function renderAsRole(role: "lecturer" | "student" | "admin") {
  const RouteRecorder = () => {
    return <RoleRedirect />;
  };

  server.use(
    http.get(`${BASE_URL}/auth/me`, () =>
      HttpResponse.json({
        user: {
          id: "u1",
          email: "test@example.com",
          full_name: "Test User",
          role,
          is_active: true,
          created_at: new Date().toISOString(),
        },
      })
    )
  );

  render(
    <AuthProvider>
      <RouteRecorder />
    </AuthProvider>
  );

  // Tunggu redirect
  await waitFor(() => {
    expect(mockReplace).toHaveBeenCalled();
  });
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe("RoleRedirect", () => {
  it("dosen (lecturer) di / redirect ke /dashboard", async () => {
    await renderAsRole("lecturer");
    expect(mockReplace).toHaveBeenCalledWith("/dashboard");
  });

  it("mahasiswa (student) di / redirect ke /dosen", async () => {
    await renderAsRole("student");
    expect(mockReplace).toHaveBeenCalledWith("/dosen");
  });

  it("admin di / redirect ke /akun", async () => {
    await renderAsRole("admin");
    expect(mockReplace).toHaveBeenCalledWith("/akun");
  });
});
