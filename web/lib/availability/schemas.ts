/**
 * Zod schema untuk validasi form ketersediaan.
 *
 * Validasi HARUS sinkron dengan validator Go di
 * internal/server/availability_handlers.go — bukan dikarang sendiri.
 *
 * Ringkasan aturan Go:
 * - day_of_week: 0-6
 * - slot_duration_min: 15-180
 * - start_time/end_time: format HH:MM, end_time > start_time
 * - jendela (end_time - start_time) >= slot_duration_min
 * - effective_from: wajib, format YYYY-MM-DD
 * - effective_to: opsional, kalau diisi harus >= effective_from
 *
 * Exception:
 * - exception_date: wajib, format YYYY-MM-DD
 * - reason: wajib
 * - is_full_day: bool
 * - Kalau is_full_day = false: start_time & end_time wajib, end_time > start_time
 */

import { z } from "zod";

/** Parse HH:MM ke menit dari tengah malam. */
function parseTimeToMinutes(time: string): number {
  const [h, m] = time.split(":").map(Number);
  return h * 60 + m;
}

/** Format YYYY-MM-DD. */
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
/** Format HH:MM (24 jam). */
const TIME_RE = /^\d{2}:\d{2}$/;

export const createRuleSchema = z
  .object({
    day_of_week: z.number().int().min(0).max(6, "Hari harus 0-6 (Minggu-Sabtu)"),
    start_time: z
      .string()
      .regex(TIME_RE, "Format jam HH:MM")
      .refine((v) => {
        const mins = parseTimeToMinutes(v);
        return mins >= 0 && mins < 24 * 60;
      }, "Jam tidak valid"),
    end_time: z
      .string()
      .regex(TIME_RE, "Format jam HH:MM")
      .refine((v) => {
        const mins = parseTimeToMinutes(v);
        return mins >= 0 && mins < 24 * 60;
      }, "Jam tidak valid"),
    slot_duration_min: z
      .number()
      .int()
      .min(15, "Durasi minimal 15 menit")
      .max(180, "Durasi maksimal 180 menit"),
    effective_from: z
      .string()
      .regex(DATE_RE, "Format tanggal YYYY-MM-DD"),
    effective_to: z
      .string()
      .regex(DATE_RE, "Format tanggal YYYY-MM-DD")
      .optional(),
  })
  .refine(
    (data) => parseTimeToMinutes(data.end_time) > parseTimeToMinutes(data.start_time),
    {
      message: "Jam selesai harus lebih besar dari jam mulai",
      path: ["end_time"],
    }
  )
  .refine(
    (data) => {
      const window =
        parseTimeToMinutes(data.end_time) - parseTimeToMinutes(data.start_time);
      return window >= data.slot_duration_min;
    },
    {
      message: "Durasi slot tidak boleh lebih besar dari rentang waktu",
      path: ["slot_duration_min"],
    }
  )
  .refine(
    (data) => {
      if (!data.effective_to) return true;
      return data.effective_to >= data.effective_from;
    },
    {
      message: "Tanggal akhir harus >= tanggal mulai",
      path: ["effective_to"],
    }
  );

export type CreateRuleValues = z.infer<typeof createRuleSchema>;

export const createExceptionSchema = z
  .object({
    exception_date: z
      .string()
      .regex(DATE_RE, "Format tanggal YYYY-MM-DD"),
    reason: z.string().min(1, "Alasan wajib diisi"),
    is_full_day: z.boolean(),
    start_time: z
      .string()
      .regex(TIME_RE, "Format jam HH:MM")
      .optional(),
    end_time: z
      .string()
      .regex(TIME_RE, "Format jam HH:MM")
      .optional(),
  })
  .refine(
    (data) => {
      if (data.is_full_day) return true;
      return !!data.start_time && !!data.end_time;
    },
    {
      message: "Jam mulai dan selesai wajib diisi jika bukan full day",
      path: ["start_time"],
    }
  )
  .refine(
    (data) => {
      if (data.is_full_day) return true;
      if (!data.start_time || !data.end_time) return true;
      return parseTimeToMinutes(data.end_time) > parseTimeToMinutes(data.start_time);
    },
    {
      message: "Jam selesai harus lebih besar dari jam mulai",
      path: ["end_time"],
    }
  );

export type CreateExceptionValues = z.infer<typeof createExceptionSchema>;

/**
 * Hitung jumlah slot yang akan dibuat dari form rule.
 * Dihitung di klien, BUKAN dari API.
 */
export function calcSlotCount(values: CreateRuleValues): number {
  const window =
    parseTimeToMinutes(values.end_time) - parseTimeToMinutes(values.start_time);
  return Math.floor(window / values.slot_duration_min);
}
