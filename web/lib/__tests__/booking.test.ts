/**
 * Unit test untuk isBookingCancelable, isSessionActionable, dan groupDashboardBookings.
 *
 * Test isBookingCancelable: batas 3 jam (tepat di bawah, tepat di atas).
 * Test isSessionActionable: batas tepat di start_at, sedikit sebelum, sedikit sesudah.
 *
 * PENTING: Test ini TIDAK butuh mock timezone untuk perbandingan instant
 * karena isSessionActionable murni pakai getTime() untuk perbandingan.
 * Untuk groupDashboardBookings, kita test batas hari UTC di dalam.
 */
import { describe, expect, it } from "vitest";
import type { BookingResponse } from "@/lib/api/types";
import { isBookingCancelable, isSessionActionable, groupDashboardBookings } from "../booking";

describe("isBookingCancelable", () => {
  const MS_3_JAM = 3 * 60 * 60 * 1000;

  it("jauh di atas 3 jam → bisa dibatalkan", () => {
    // Slot mulai 24 jam dari sekarang → jauh di atas batas.
    const slotStart = new Date(Date.now() + 24 * 60 * 60 * 1000);
    expect(isBookingCancelable(slotStart)).toBe(true);
  });

  it("tepat di atas 3 jam → bisa dibatalkan", () => {
    // Slot mulai 3 jam + 1 detik dari sekarang → sedikit di atas batas.
    const slotStart = new Date(Date.now() + MS_3_JAM + 1000);
    expect(isBookingCancelable(slotStart)).toBe(true);
  });

  it("sedikit di bawah 3 jam → TIDAK bisa dibatalkan", () => {
    // Slot mulai 3 jam - 1 detik dari sekarang → sedikit di bawah batas.
    const slotStart = new Date(Date.now() + MS_3_JAM - 1000);
    expect(isBookingCancelable(slotStart)).toBe(false);
  });

  it("tepat 3 jam → TIDAK bisa dibatalkan", () => {
    // Slot mulai tepat 3 jam dari sekarang.
    // getTime() - Date.now() == 3*3600*1000 → TIDAK > → false.
    const slotStart = new Date(Date.now() + MS_3_JAM);
    expect(isBookingCancelable(slotStart)).toBe(false);
  });

  it("sedikit di atas 3 jam (1ms) → bisa dibatalkan", () => {
    // Slot mulai 3 jam + 1ms dari sekarang.
    const slotStart = new Date(Date.now() + MS_3_JAM + 1);
    expect(isBookingCancelable(slotStart)).toBe(true);
  });

  it("slot sudah lewat → TIDAK bisa dibatalkan", () => {
    // Slot mulai 1 jam yang lalu.
    const slotStart = new Date(Date.now() - 60 * 60 * 1000);
    expect(isBookingCancelable(slotStart)).toBe(false);
  });
});

describe("isSessionActionable", () => {
  it("tepat di start_at → bisa ditindaklanjuti", () => {
    // Slot mulai tepat sekarang → startAt.getTime() == Date.now() → >= → true.
    const slotStart = new Date(Date.now());
    expect(isSessionActionable(slotStart)).toBe(true);
  });

  it("sedikit sebelum start_at → TIDAK bisa ditindaklanjuti", () => {
    // Slot mulai 1 detik lagi.
    const slotStart = new Date(Date.now() + 1000);
    expect(isSessionActionable(slotStart)).toBe(false);
  });

  it("sedikit sesudah start_at → bisa ditindaklanjuti", () => {
    // Slot mulai 1 detik yang lalu.
    const slotStart = new Date(Date.now() - 1000);
    expect(isSessionActionable(slotStart)).toBe(true);
  });

  it("satu jam yang lalu → bisa ditindaklanjuti", () => {
    const slotStart = new Date(Date.now() - 60 * 60 * 1000);
    expect(isSessionActionable(slotStart)).toBe(true);
  });

  it("satu jam ke depan → TIDAK bisa ditindaklanjuti", () => {
    const slotStart = new Date(Date.now() + 60 * 60 * 1000);
    expect(isSessionActionable(slotStart)).toBe(false);
  });
});

describe("groupDashboardBookings", () => {
  // Helper buat bikin booking mock
  function makeBooking(
    id: string,
    startAt: Date,
    status: "confirmed" | "cancelled" | "completed" | "no_show" = "confirmed"
  ): BookingResponse {
    return {
      id,
      status,
      topic: `Topic ${id}`,
      description: "",
      created_at: new Date().toISOString(),
      slot: {
        id: `slot-${id}`,
        start_at: startAt.toISOString(),
        end_at: new Date(startAt.getTime() + 30 * 60 * 1000).toISOString(),
      },
      student: {
        id: "student-1",
        full_name: "Mahasiswa Test",
      },
    };
  }

  it("booking di masa lalu → needsFollowUp", () => {
    const now = new Date("2026-09-11T12:00:00Z"); // 19:00 WIB
    const yesterday = new Date("2026-09-11T08:00:00Z"); // 15:00 WIB kemarin
    const booking = makeBooking("b1", yesterday);

    const result = groupDashboardBookings([booking], now);

    expect(result.needsFollowUp).toHaveLength(1);
    expect(result.needsFollowUp[0].id).toBe("b1");
    expect(result.today).toHaveLength(0);
    expect(result.upcoming.size).toBe(0);
  });

  it("booking hari ini tapi belum lewat → today", () => {
    // now: 2026-09-11T12:00:00Z = 19:00 WIB
    // booking: 2026-09-11T14:00:00Z = 21:00 WIB (hari yang sama)
    const now = new Date("2026-09-11T12:00:00Z");
    const todaySlot = new Date("2026-09-11T14:00:00Z"); // 21:00 WIB
    const booking = makeBooking("b1", todaySlot);

    const result = groupDashboardBookings([booking], now);

    expect(result.today).toHaveLength(1);
    expect(result.today[0].id).toBe("b1");
    expect(result.needsFollowUp).toHaveLength(0);
    expect(result.upcoming.size).toBe(0);
  });

  it("booking hari besok → upcoming", () => {
    // now: 2026-09-11T12:00:00Z = 19:00 WIB
    // booking: 2026-09-12T08:00:00Z = 15:00 WIB besok
    const now = new Date("2026-09-11T12:00:00Z");
    const tomorrowSlot = new Date("2026-09-12T08:00:00Z");
    const booking = makeBooking("b1", tomorrowSlot);

    const result = groupDashboardBookings([booking], now);

    expect(result.upcoming.size).toBe(1);
    expect(result.upcoming.get("2026-09-12")).toHaveLength(1);
    expect(result.today).toHaveLength(0);
    expect(result.needsFollowUp).toHaveLength(0);
  });

  it("booking jam 17:00 UTC lintas batas hari WIB → upcoming besok", () => {
    // KLAS BUG: jam 17:00 UTC = 00:00 WIB keesokan harinya
    // now: 2026-09-10T16:00:00Z = 23:00 WIB (10 Sep)
    // booking: 2026-09-10T17:00:00Z = 00:00 WIB (11 Sep) → HARUS upcoming besok
    const now = new Date("2026-09-10T16:00:00Z");
    const slotAtMidnight = new Date("2026-09-10T17:00:00Z"); // 00:00 WIB 11 Sep
    const booking = makeBooking("b1", slotAtMidnight);

    const result = groupDashboardBookings([booking], now);

    // booking harus masuk upcoming karena di WIB itu 11 Sep, bukan 10 Sep
    expect(result.upcoming.size).toBe(1);
    expect(result.upcoming.get("2026-09-11")).toHaveLength(1);
    expect(result.today).toHaveLength(0);
    expect(result.needsFollowUp).toHaveLength(0);
  });

  it("booking jam 16:59 UTC tetap hari kemarin di WIB → today", () => {
    // now: 2026-09-10T15:00:00Z = 22:00 WIB (10 Sep)
    // booking: 2026-09-10T16:59:00Z = 23:59 WIB (10 Sep) → masih hari yang sama
    const now = new Date("2026-09-10T15:00:00Z");
    const slotAtAlmostMidnight = new Date("2026-09-10T16:59:00Z"); // 23:59 WIB 10 Sep
    const booking = makeBooking("b1", slotAtAlmostMidnight);

    const result = groupDashboardBookings([booking], now);

    expect(result.today).toHaveLength(1);
    expect(result.upcoming.size).toBe(0);
  });

  it("multiple bookings di berbagai bucket → terkelompok dengan benar", () => {
    // now: 2026-09-11T12:00:00Z = 19:00 WIB
    const now = new Date("2026-09-11T12:00:00Z");
    const bookings = [
      // Needs follow up (sudah lewat)
      makeBooking("b1", new Date("2026-09-11T08:00:00Z")), // 15:00 WIB kemarin
      // Today
      makeBooking("b2", new Date("2026-09-11T14:00:00Z")), // 21:00 WIB
      makeBooking("b3", new Date("2026-09-11T15:00:00Z")), // 22:00 WIB
      // Upcoming besok
      makeBooking("b4", new Date("2026-09-12T08:00:00Z")), // 15:00 WIB besok
      makeBooking("b5", new Date("2026-09-13T10:00:00Z")), // 17:00 WIB lusa
      // Non-confirmed diabaikan
      makeBooking("b6", new Date("2026-09-11T14:00:00Z"), "cancelled"),
    ];

    const result = groupDashboardBookings(bookings, now);

    expect(result.needsFollowUp).toHaveLength(1);
    expect(result.needsFollowUp[0].id).toBe("b1");
    expect(result.today).toHaveLength(2);
    expect(result.upcoming.size).toBe(2); // 2 tanggal berbeda
    expect(result.upcoming.get("2026-09-12")).toHaveLength(1);
    expect(result.upcoming.get("2026-09-13")).toHaveLength(1);
  });

  it("upcoming diurutkan ascending per tanggal", () => {
    const now = new Date("2026-09-11T12:00:00Z");
    const bookings = [
      makeBooking("b1", new Date("2026-09-14T08:00:00Z")), // lusa
      makeBooking("b2", new Date("2026-09-12T08:00:00Z")), // besok
      makeBooking("b3", new Date("2026-09-13T08:00:00Z")), // 2 hari lagi
    ];

    const result = groupDashboardBookings(bookings, now);
    const dateKeys = [...result.upcoming.keys()];

    expect(dateKeys).toEqual(["2026-09-12", "2026-09-13", "2026-09-14"]);
  });
});
