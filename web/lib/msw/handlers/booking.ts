/**
 * MSW handlers untuk booking API endpoints.
 *
 * Dipakai di test untuk intercept HTTP calls.
 */
import { http, HttpResponse } from "msw";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

// In-memory store untuk test state.
interface BookingStore {
  bookings: Map<string, unknown>;
  nextId: number;
}

const store: BookingStore = {
  bookings: new Map(),
  nextId: 1,
};

// Reset store untuk setiap test.
export function resetBookingStore() {
  store.bookings.clear();
  store.nextId = 1;
}

// Set initial bookings dari luar (misal di test setup).
export function setBookings(bookings: Array<{ id: string; [key: string]: unknown }>) {
  store.bookings.clear();
  for (const b of bookings) {
    store.bookings.set(b.id, b);
    const numId = parseInt(b.id.replace(/\D/g, ""), 10);
    if (!isNaN(numId) && numId >= store.nextId) {
      store.nextId = numId + 1;
    }
  }
}

export const bookingHandlers = [
  // POST /api/v1/bookings — create booking
  http.post(`${BASE_URL}/bookings`, async ({ request }) => {
    const body = (await request.json()) as { slot_id: string; topic: string; description?: string };
    const idempotencyKey = request.headers.get("Idempotency-Key");

    // Simulasi error berdasarkan body atau header.
    // Ini bisa di-override di test dengan `server.use(...)`.

    // Slot already booked simulation.
    if (body.slot_id === "slot-already-booked") {
      return HttpResponse.json(
        { error: { code: "SLOT_ALREADY_BOOKED", message: "Slot sudah dipesan orang lain" } },
        { status: 409 }
      );
    }

    // Slot too soon simulation.
    if (body.slot_id === "slot-too-soon") {
      return HttpResponse.json(
        { error: { code: "SLOT_TOO_SOON", message: "Minimal 60 menit sebelum konsultasi" } },
        { status: 422 }
      );
    }

    // Booking limit reached simulation.
    if (body.slot_id === "slot-limit-reached") {
      return HttpResponse.json(
        { error: { code: "BOOKING_LIMIT_REACHED", message: "Batas 3 booking aktif sudah tercapai" } },
        { status: 409 }
      );
    }

    // Sukses.
    const id = `booking-${store.nextId++}`;
    const booking = {
      id,
      status: "confirmed",
      topic: body.topic,
      description: body.description ?? "",
      created_at: new Date().toISOString(),
      slot: {
        id: body.slot_id,
        start_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
        end_at: new Date(Date.now() + 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
      },
      lecturer: {
        id: "lecturer-1",
        full_name: "Dr. Dosen",
      },
    };
    store.bookings.set(id, booking);

    return HttpResponse.json({ id }, { status: 201 });
  }),

  // GET /api/v1/bookings — list bookings
  http.get(`${BASE_URL}/bookings`, ({ request }) => {
    const url = new URL(request.url);
    const status = url.searchParams.get("status");
    const page = parseInt(url.searchParams.get("page") ?? "1", 10);
    const perPage = parseInt(url.searchParams.get("per_page") ?? "20", 10);

    let bookings = Array.from(store.bookings.values()) as Array<{
      id: string;
      status: string;
      [key: string]: unknown;
    }>;

    if (status && status !== "all") {
      bookings = bookings.filter((b) => b.status === status);
    }

    // Sort by created_at descending.
    bookings.sort((a, b) => {
      const aTime = new Date(a.created_at as string).getTime();
      const bTime = new Date(b.created_at as string).getTime();
      return bTime - aTime;
    });

    const total = bookings.length;
    const start = (page - 1) * perPage;
    const paginatedBookings = bookings.slice(start, start + perPage);

    return HttpResponse.json({
      bookings: paginatedBookings,
      page,
      per_page: perPage,
      total,
    });
  }),

  // GET /api/v1/bookings/:id — get single booking
  http.get(`${BASE_URL}/bookings/:id`, ({ params }) => {
    const id = params.id as string;
    const booking = store.bookings.get(id);
    if (!booking) {
      return HttpResponse.json(
        { error: { code: "BOOKING_NOT_FOUND", message: "Booking tidak ditemukan" } },
        { status: 404 }
      );
    }
    return HttpResponse.json(booking);
  }),

  // PATCH /api/v1/bookings/:id/cancel — cancel booking
  http.patch(`${BASE_URL}/bookings/:id/cancel`, async ({ params, request }) => {
    const id = params.id as string;
    const booking = store.bookings.get(id) as { status: string; [key: string]: unknown } | undefined;

    if (!booking) {
      return HttpResponse.json(
        { error: { code: "BOOKING_NOT_FOUND", message: "Booking tidak ditemukan" } },
        { status: 404 }
      );
    }

    if (booking.status === "cancelled") {
      return HttpResponse.json(
        { error: { code: "BOOKING_ALREADY_CANCELLED", message: "Booking sudah dibatalkan" } },
        { status: 409 }
      );
    }

    if (booking.status === "completed" || booking.status === "no_show") {
      return HttpResponse.json(
        { error: { code: "BOOKING_ALREADY_FINALIZED", message: "Booking sudah diselesaikan" } },
        { status: 409 }
      );
    }

    const body = (await request.json().catch(() => ({}))) as { reason?: string };
    const cancelledBooking = {
      ...booking,
      status: "cancelled",
      cancelled_at: new Date().toISOString(),
      cancel_reason: body.reason ?? null,
    };
    store.bookings.set(id, cancelledBooking);

    return HttpResponse.json(cancelledBooking);
  }),
];
