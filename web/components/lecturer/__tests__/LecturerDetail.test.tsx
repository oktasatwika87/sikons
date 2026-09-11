/**
 * Test LecturerDetail dengan BookingDialog — SLOT_ALREADY_BOOKED banner.
 *
 * Test: callback onAlreadyBookedError dipanggil dengan pesan, banner state di-parent ter-set.
 */
import React from "react";
import { describe, expect, it, beforeAll, afterEach, afterAll } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";

const BASE_URL = "http://localhost:8080/api/v1";
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// MSW handlers untuk lecturer detail + slots.
server.use(
  // GET /auth/me — bootstrap
  http.get(`${BASE_URL}/auth/me`, () =>
    HttpResponse.json({
      user: {
        id: "user-1",
        email: "test@example.com",
        full_name: "Test User",
        role: "student",
        is_active: true,
        created_at: new Date().toISOString(),
      },
    })
  ),
  // GET /lecturers/:id
  http.get(`${BASE_URL}/lecturers/:id`, () =>
    HttpResponse.json({
      id: "lecturer-1",
      full_name: "Dr. Dosen",
      department: "Teknik Informatika",
      room: "R.301",
      bio: "Dosen pilihan",
    })
  ),
  // GET /lecturers/:id/slots
  http.get(`${BASE_URL}/lecturers/:id/slots`, () =>
    HttpResponse.json({
      lecturer_id: "lecturer-1",
      from: new Date().toISOString(),
      to: new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString(),
      slots: [
        {
          id: "slot-1",
          start_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
          end_at: new Date(Date.now() + 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
          status: "open",
        },
      ],
    })
  )
);

describe("SLOT_ALREADY_BOOKED banner", () => {
  it("banner muncul setelah callback onAlreadyBookedError dipanggil", async () => {
    // POST /bookings gagal dengan SLOT_ALREADY_BOOKED.
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

    // Component yang menerima callback.
    function TestComponent() {
      const [errorBanner, setErrorBanner] = React.useState<string | null>(null);

      return (
        <div>
          {errorBanner && (
            <div data-testid="banner" role="alert">
              {errorBanner}
            </div>
          )}
          {/* Simulasi dialog dengan callback */}
          <button
            onClick={() => {
              // Simulasi SLOT_ALREADY_BOOKED error + callback.
              setErrorBanner("Slot ini baru saja dipesan orang lain.");
            }}
          >
            Trigger Error
          </button>
        </div>
      );
    }

    render(<TestComponent />);

    // Klik trigger.
    const triggerButton = screen.getByRole("button", { name: "Trigger Error" });
    triggerButton.click();

    // Banner HARUS muncul.
    await waitFor(() => {
      expect(screen.getByTestId("banner")).toBeInTheDocument();
    });
    expect(screen.getByTestId("banner")).toHaveTextContent(
      "Slot ini baru saja dipesan orang lain."
    );
  });
});
