/**
 * Unit test untuk isBookingCancelable.
 *
 * Test batas 3 jam (tepat di bawah, tepat di atas).
 *
 * PENTING: Test ini TIDAK butuh mock timezone karena isBookingCancelable
 * murni pakai getTime() untuk perbandingan — tidak pakai formatting apa pun.
 */
import { describe, expect, it } from "vitest";
import { isBookingCancelable } from "../booking";

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
