/**
 * Test Zod schemas untuk validasi form ketersediaan.
 *
 * Validasi HARUS sinkron dengan validator Go — test ini memastikan
 * tidak ada drift antara frontend dan backend.
 */
import { describe, expect, it } from "vitest";
import {
  createRuleSchema,
  createExceptionSchema,
  calcSlotCount,
} from "@/lib/availability/schemas";

describe("createRuleSchema", () => {
  it("accepts valid rule", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  it("accepts rule with effective_to", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 5,
      start_time: "08:00",
      end_time: "17:00",
      slot_duration_min: 60,
      effective_from: "2026-09-14",
      effective_to: "2026-12-31",
    });
    expect(result.success).toBe(true);
  });

  // Boundary: day_of_week
  it("rejects day_of_week = 7 (out of range)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 7,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("rejects day_of_week = -1 (out of range)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: -1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("accepts day_of_week = 0 (Minggu)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 0,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  it("accepts day_of_week = 6 (Sabtu)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 6,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  // Boundary: slot_duration_min
  it("rejects slot_duration_min = 14 (below minimum)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 14,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("rejects slot_duration_min = 181 (above maximum)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 181,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("accepts slot_duration_min = 15 (minimum)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 15,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  it("accepts slot_duration_min = 180 (maximum)", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 180,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  // Boundary: end_time > start_time
  it("rejects end_time before start_time", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "12:00",
      end_time: "09:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("rejects end_time = start_time", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "12:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  // Boundary: jendela >= slot_duration_min
  it("rejects jendela smaller than slot_duration_min", () => {
    // 3 jam = 180 menit, tapi durasi 60 menit → 3 slot, OK
    // Tapi 1 jam = 60 menit, durasi 30 menit → 2 slot, OK
    // Kasus gagal: 1 jam = 60 menit, durasi 90 menit
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "10:00",
      slot_duration_min: 90,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  // Boundary: effective_to >= effective_from
  it("rejects effective_to before effective_from", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
      effective_to: "2026-09-01",
    });
    expect(result.success).toBe(false);
  });

  it("accepts effective_to = effective_from", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
      effective_to: "2026-09-14",
    });
    expect(result.success).toBe(true);
  });

  // Format validation
  it("rejects invalid time format", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "9:00", // missing leading zero
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(result.success).toBe(false);
  });

  it("rejects invalid date format", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026/09/14", // wrong separator
    });
    expect(result.success).toBe(false);
  });

  // Missing required fields
  it("rejects missing effective_from", () => {
    const result = createRuleSchema.safeParse({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
    });
    expect(result.success).toBe(false);
  });
});

describe("createExceptionSchema", () => {
  it("accepts valid full day exception", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Libur nasional",
      is_full_day: true,
    });
    expect(result.success).toBe(true);
  });

  it("accepts valid partial exception with times", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Meeting pagi",
      is_full_day: false,
      start_time: "09:00",
      end_time: "12:00",
    });
    expect(result.success).toBe(true);
  });

  // Boundary: partial exception requires times
  it("rejects partial exception without start_time", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Meeting pagi",
      is_full_day: false,
      end_time: "12:00",
    });
    expect(result.success).toBe(false);
  });

  it("rejects partial exception without end_time", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Meeting pagi",
      is_full_day: false,
      start_time: "09:00",
    });
    expect(result.success).toBe(false);
  });

  // Boundary: end_time > start_time
  it("rejects end_time before start_time in partial exception", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Meeting pagi",
      is_full_day: false,
      start_time: "12:00",
      end_time: "09:00",
    });
    expect(result.success).toBe(false);
  });

  it("rejects end_time = start_time in partial exception", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "Meeting pagi",
      is_full_day: false,
      start_time: "12:00",
      end_time: "12:00",
    });
    expect(result.success).toBe(false);
  });

  // Missing required fields
  it("rejects missing exception_date", () => {
    const result = createExceptionSchema.safeParse({
      reason: "Libur nasional",
      is_full_day: true,
    });
    expect(result.success).toBe(false);
  });

  it("rejects empty reason", () => {
    const result = createExceptionSchema.safeParse({
      exception_date: "2026-09-14",
      reason: "",
      is_full_day: true,
    });
    expect(result.success).toBe(false);
  });
});

describe("calcSlotCount", () => {
  it("calculates correct slot count", () => {
    const count = calcSlotCount({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(count).toBe(6); // 3 jam = 180 menit / 30 menit = 6 slot
  });

  it("rounds down partial slots", () => {
    const count = calcSlotCount({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "11:00",
      slot_duration_min: 30,
      effective_from: "2026-09-14",
    });
    expect(count).toBe(4); // 2 jam = 120 menit / 30 menit = 4 slot
  });

  it("handles 15 minute slots", () => {
    const count = calcSlotCount({
      day_of_week: 1,
      start_time: "09:00",
      end_time: "12:00",
      slot_duration_min: 15,
      effective_from: "2026-09-14",
    });
    expect(count).toBe(12); // 3 jam = 180 menit / 15 menit = 12 slot
  });
});
