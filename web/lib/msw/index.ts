/**
 * MSW setup untuk test.
 *
 * Export semua handlers yang bisa dipakai di test files.
 */
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { bookingHandlers, resetBookingStore, setBookings } from "./handlers/booking";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

// Auth handlers untuk bootstrap.
function authHandlers() {
  return [
    // GET /api/v1/auth/me — bootstrap
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
  ];
}

// Default server instance.
export const server = setupServer(
  ...authHandlers(),
  ...bookingHandlers,
);

// Reset helper yang bisa dipanggil di beforeEach.
export { resetBookingStore, setBookings };
