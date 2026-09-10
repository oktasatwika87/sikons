/**
 * Daftar department yang ditampilkan di filter dropdown /dosen.
 *
 * PENTING: nilai ini HARUS sama persis dengan yang ada di database — lihat
 * `cmd/seed/main.go`. Kalau seed berubah, update sini juga. Prinsipnya
 * sama dengan DTO yang dicek silang ke struct Go di M5a (lihat
 * RINGKASAN-M5C keputusan #5).
 *
 * Kalau di kemudian hari ada endpoint backend untuk daftar department
 * (mis. setelah M5d), ganti nilai ini jadi fetch, BUKAN hardcode.
 *
 * Sumber nilai saat ini:
 *   cmd/seed/main.go baris 90–91:
 *     "Informatika", "Sistem Informasi"
 */
export const DEPARTMENTS: readonly string[] = [
  "Informatika",
  "Sistem Informasi",
] as const;
