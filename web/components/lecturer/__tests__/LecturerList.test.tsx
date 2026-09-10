/**
 * Test LecturerList — verifikasi filter dari URL diteruskan ke endpoint.
 *
 * MSW mengintersep fetch dan merekam URL yang diminta; kita verifikasi
 * parameter URL (department, q, page) sampai utuh ke query string backend.
 *
 * next/navigation di-mock dengan vi.mock karena useSearchParams/useRouter
 * bukan layer HTTP — pola yang sama dengan LoginForm.test.tsx.
 */
import { type ReactNode } from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";

const mockRouter = {
  push: vi.fn(),
  replace: vi.fn(),
  refresh: vi.fn(),
};
let mockSearchParams = new URLSearchParams("");
let mockPathname = "/dosen";

vi.mock("next/navigation", () => ({
  useRouter: () => mockRouter,
  useSearchParams: () => mockSearchParams,
  usePathname: () => mockPathname,
}));

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  mockRouter.push.mockReset();
  mockRouter.replace.mockReset();
  mockRefresh?.mockReset?.();
  mockSearchParams = new URLSearchParams("");
  mockPathname = "/dosen";
});
afterAll(() => server.close());

const mockRefresh = mockRouter.refresh;

// Impor SETELAH vi.mock — supaya hook dapat versi yang sudah di-mock.
const { LecturerList } = await import("@/components/lecturer/LecturerList");

interface RequestLog {
  url: string;
  method: string;
}

/**
 * Bootstrap MSW: /auth/me 401 (belum login — endpoint publik), /lecturers
 * mengembalikan data fixture dan MEREKAM URL yang diminta.
 */
function setupLecturersHandler(log: RequestLog[]) {
  server.use(
    http.get(`${BASE_URL}/auth/me`, () =>
      HttpResponse.json(
        { error: { code: "UNAUTHORIZED", message: "" } },
        { status: 401 }
      )
    ),
    http.get(`${BASE_URL}/lecturers`, ({ request }) => {
      log.push({ url: request.url, method: request.method });
      return HttpResponse.json({
        lecturers: [
          {
            id: "dosen-1",
            full_name: "Dr. Budi Santoso",
            department: "Informatika",
            room: "Gedung C, Ruang 301",
            bio: "Bio singkat",
          },
        ],
        page: 1,
        per_page: 20,
        total: 1,
      });
    })
  );
}

/**
 * Render dengan QueryClientProvider + AuthProvider + Suspense boundary.
 */
function renderWithProviders(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{ui}</AuthProvider>
    </QueryClientProvider>
  );
}

describe("LecturerList — filter URL diteruskan ke endpoint", () => {
  it("department, q, dan page dari URL muncul sebagai query param /lecturers", async () => {
    mockSearchParams = new URLSearchParams(
      "department=Informatika&q=bud&page=2"
    );
    const log: RequestLog[] = [];
    setupLecturersHandler(log);

    renderWithProviders(
      <LecturerList />
    );

    // Tunggu sampai fetch pertama dikirim dan balasan muncul di DOM.
    await waitFor(() => {
      expect(screen.getByText("Dr. Budi Santoso")).toBeInTheDocument();
    });

    expect(log).toHaveLength(1);
    const url = new URL(log[0].url);
    // useApiFetch menambahkan prefix NEXT_PUBLIC_API_URL — di test ini
    // default-nya http://localhost:8080/api/v1.
    expect(url.pathname).toBe("/api/v1/lecturers");
    expect(url.searchParams.get("department")).toBe("Informatika");
    expect(url.searchParams.get("q")).toBe("bud");
    expect(url.searchParams.get("page")).toBe("2");
  });

  it("URL tanpa filter → /lecturers tanpa query string", async () => {
    mockSearchParams = new URLSearchParams("");
    const log: RequestLog[] = [];
    setupLecturersHandler(log);

    renderWithProviders(<LecturerList />);

    await waitFor(() => {
      expect(screen.getByText("Dr. Budi Santoso")).toBeInTheDocument();
    });

    expect(log).toHaveLength(1);
    const url = new URL(log[0].url);
    expect(url.pathname).toBe("/api/v1/lecturers");
    expect(url.search).toBe("");
  });
});
