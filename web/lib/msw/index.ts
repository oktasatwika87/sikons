/**
 * MSW setup untuk test.
 *
 * Export semua handlers yang bisa dipakai di test files.
 */
import { setupServer } from "msw/node";
import { bookingHandlers, resetBookingStore, setBookings } from "./handlers/booking";
import { sessionHandlers } from "./session";

// Auth handlers untuk bootstrap: refresh (cookie) lalu GET /me, sama dengan
// alur AuthProvider yang sebenarnya. Lihat lib/msw/session.ts.
function authHandlers() {
  return sessionHandlers({
    user: {
      id: "user-1",
      email: "test@example.com",
      full_name: "Test User",
      role: "student",
      is_active: true,
      created_at: new Date().toISOString(),
    },
  });
}

// Default server instance.
export const server = setupServer(
  ...authHandlers(),
  ...bookingHandlers,
);

// Reset helper yang bisa dipanggil di beforeEach.
export { resetBookingStore, setBookings };
