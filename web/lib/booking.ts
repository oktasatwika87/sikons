/**
 * Helper functions untuk booking.
 *
 * PENTING: Fungsi perbandingan waktu di sini BUKAN pakai lib/date.ts.
 * lib/date.ts HANYA untuk menampilkan tanggal dalam format yang benar
 * (dengan zona Asia/Jakarta). Fungsi di sini murni untuk LOGIC
 * perbandingan waktu.
 */

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
