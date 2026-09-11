/**
 * Helper functions untuk booking.
 *
 * PENTING: Fungsi perbandingan waktu di sini BUKAN pakai lib/date.ts.
 * lib/date.ts HANYA untuk menampilkan tanggal dalam format yang benar
 * (dengan zona Asia/Jakarta). Fungsi di sini murni untuk LOGIC
 * perbandingan waktu.
 */

import type { BookingResponse } from "@/lib/api/types";
import { isSameJakartaDay, jakartaDateKey } from "@/lib/date";

/**
 * Cek apakah booking masih bisa dibatalkan.
 *
 * Mahasiswa tidak boleh cancel booking yang kurang dari 3 jam sebelum jadwal.
 *
 * Perbandingan pakai getTime() murni karena:
 * - Tidak butuh format tampilan (bukan untuk ditampilkan)
 * - Date.now() dan slotStartAt.getTime() sama-sama UTC milliseconds
 * - Tidak terpengaruh zona browser
 *
 * @param slotStartAt Tanggal mulai slot (Date object dari parsing RFC3339)
 * @returns true kalau masih bisa dibatalkan, false kalau sudah lewat H-3 jam
 */
export function isBookingCancelable(slotStartAt: Date): boolean {
  const msUntilSlot = slotStartAt.getTime() - Date.now();
  const minCancelMs = 3 * 60 * 60 * 1000; // 3 jam dalam milidetik
  return msUntilSlot > minCancelMs;
}

/**
 * Cek apakah sesi sudah bisa ditindaklanjuti (tandai selesai / tidak hadir).
 *
 * Dosen bisa menandai sesi selesai atau tidak hadir HANYA setelah waktu mulai
 * slot berlalu. Perbandingan instant murni (epoch ms) karena ini bukan
 * operasi kalender/tampilan.
 *
 * @param slotStartAt Tanggal mulai slot (Date object dari parsing RFC3339)
 * @returns true kalau sekarang sudah melewati atau sama dengan startAt
 */
export function isSessionActionable(slotStartAt: Date): boolean {
  return Date.now() >= slotStartAt.getTime();
}

/**
 * Kelompokkan booking confirmed ke tiga bucket untuk dashboard dosen.
 *
 * Pengelompokan berdasarkan waktu saat data diambil (parameter `nowInstant`),
 * BUKAN Date.now() di dalam fungsi — supaya bisa ditest tanpa mock timer.
 *
 * Tiga bucket SALING LEPAS (tidak tumpang tindih):
 * - needsFollowUp: confirmed DAN startAt < now (sudah lewat, kapan pun)
 * - today: confirmed DAN startAt >= now DAN jatuh di tanggal yang sama dengan now (WIB)
 * - upcoming: confirmed DAN startAt >= now DAN BUKAN tanggal yang sama dengan now (WIB)
 *
 * @param bookings Daftar booking dari GET /bookings
 * @param nowInstant Instant referensi untuk pengelompokan (Date.now() saat fetch)
 * @returns Tiga array booking yang saling lepas
 */
export function groupDashboardBookings(
  bookings: BookingResponse[],
  nowInstant: Date
): {
  needsFollowUp: BookingResponse[];
  today: BookingResponse[];
  upcoming: Map<string, BookingResponse[]>; // key: tanggal WIB (YYYY-MM-DD)
} {
  const confirmed = bookings.filter((b) => b.status === "confirmed");
  const nowMs = nowInstant.getTime();

  const needsFollowUp: BookingResponse[] = [];
  const today: BookingResponse[] = [];
  const upcomingByDate = new Map<string, BookingResponse[]>();

  for (const booking of confirmed) {
    const startAt = new Date(booking.slot.start_at);
    const startAtMs = startAt.getTime();

    // Beda antara (a) dan (b)/(c): perbandingan instant murni
    if (startAtMs < nowMs) {
      needsFollowUp.push(booking);
      continue;
    }

    // Di sini startAt >= now. Cek apakah hari yang sama dengan now (WIB).
    // Ini operasi kalender — butuh zona Asia/Jakarta.
    if (isSameJakartaDay(startAt, nowInstant)) {
      today.push(booking);
    } else {
      // upcoming — kelompokkan per tanggal WIB
      const dateKey = jakartaDateKey(startAt);
      const list = upcomingByDate.get(dateKey);
      if (list) {
        list.push(booking);
      } else {
        upcomingByDate.set(dateKey, [booking]);
      }
    }
  }

  // Urutkan upcoming per tanggal
  const upcoming = new Map(
    [...upcomingByDate.entries()].sort(([a], [b]) => a.localeCompare(b))
  );

  return { needsFollowUp, today, upcoming };
}

/**
 * Label status booking dalam Bahasa Indonesia.
 */
export function bookingStatusLabel(status: string): string {
  switch (status) {
    case "confirmed":
      return "Akan Datang";
    case "cancelled":
      return "Dibatalkan";
    case "completed":
      return "Selesai";
    case "no_show":
      return "Tidak Hadir";
    default:
      return status;
  }
}

/**
 * CSS class untuk badge status booking.
 */
export function bookingStatusClass(status: string): string {
  switch (status) {
    case "confirmed":
      return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
    case "cancelled":
      return "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200";
    case "completed":
      return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
    case "no_show":
      return "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200";
    default:
      return "bg-gray-100 text-gray-800";
  }
}
