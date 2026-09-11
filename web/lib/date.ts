/**
 * Utilitas tanggal/waktu terpusat — SEMUA tampilan waktu dari backend (RFC3339
 * UTC) di frontend WAJIB lewat file ini.
 *
 * Aturan keras (lihat RINGKASAN-M5C keputusan #7):
 *  - Backend menyimpan semua waktu sebagai `timestamptz` UTC.
 *  - Zona tampilan adalah Asia/Jakarta, TETAP — tidak ikut zona browser.
 *  - `new Date(...).toLocaleString()` itu IKUT zona browser, jadi DILARANG.
 *  - Pustaka: `date-fns` + `date-fns-tz` (konsisten dengan date-fns yang sudah
 *    disebut di brief).
 *
 * Pelajaran dari debugging awal (lihat commit ini):
 *  - `toZonedTime(date, tz)` MENGUBAH internal timestamp Date supaya method
 *    `getDate()`/`getHours()`/dll. mengembalikan wall-time di zona target.
 *  - TAPI `format(date, ...)` dari date-fns SELALU pakai zona LOKAL runtime.
 *    Di jsdom (TZ=UTC) atau macOS dev (TZ=Asia/Makassar) hasilnya beda.
 *  - Solusi: SELALU pakai `formatInTimeZone(date, TIMEZONE, fmt)` untuk
 *    ekstraksi komponen tanggal yang akan ditampilkan — TIDAK ada
 *    `format(zonedDate, ...)`.
 *
 * Batas hari Asia/Jakarta = 17:00 UTC (= 00:00 WIB keesokan harinya).
 * Siapa pun yang menulis tampilan waktu di SIKONS, lewat sini dulu.
 */

import { formatInTimeZone, fromZonedTime } from "date-fns-tz";
import { id as localeId } from "date-fns/locale";

/** Zona tampilan tetap — single source of truth. */
export const TIMEZONE = "Asia/Jakarta";

/** Hari dalam Bahasa Indonesia (Senin..Minggu) untuk label kolom kalender. */
const ISO_DAY_NAMES_LONG: Record<number, string> = {
  1: "Senin",
  2: "Selasa",
  3: "Rabu",
  4: "Kamis",
  5: "Jumat",
  6: "Sabtu",
  7: "Minggu",
};

/**
 * Parse input string|Date → Date UTC. `new Date(rfc3339)` selalu UTC untuk
 * string dengan `Z` atau `±HH:MM` — aman dipakai tanpa Zone.
 */
function parseRFC3339(value: string | Date): Date {
  if (value instanceof Date) return value;
  return new Date(value);
}

/**
 * ISO weekday (Senin=1, Minggu=7) untuk `value`, dihitung di zona Jakarta.
 * Token `i` di date-fns v4 sudah ISO weekday — lihat catatan revisi di
 * header file ini.
 */
function isoWeekdayJakarta(value: string | Date): number {
  return Number(formatInTimeZone(parseRFC3339(value), TIMEZONE, "i"));
}

/**
 * Format tanggal singkat "10 Sep" di zona Asia/Jakarta — untuk header
 * kolom hari di kalender (di bawah label "Senin"). Tidak sertakan tahun
 * karena minggu kalender sudah ditampilkan di header utama.
 */
export function formatJakartaShortDate(value: string | Date): string {
  return formatInTimeZone(parseRFC3339(value), TIMEZONE, "d MMM", {
    locale: localeId,
  });
}

/**
 * Format jam singkat "HH:mm" di zona Asia/Jakarta — untuk tampilan slot
 * di kalender. Tidak menampilkan zona agar tidak memenuhi UI.
 *
 * Slot 17:00 UTC → "00:00" (keesokan harinya di WIB).
 */
export function formatJakartaTime(value: string | Date): string {
  return formatInTimeZone(parseRFC3339(value), TIMEZONE, "HH:mm");
}

/**
 * Format tanggal lengkap "Jumat, 11 September 2026" — untuk header minggu
 * kalender. Pakai locale id dari date-fns.
 */
export function formatJakartaLongDate(value: string | Date): string {
  return formatInTimeZone(parseRFC3339(value), TIMEZONE, "EEEE, d MMMM yyyy", {
    locale: localeId,
  });
}

/**
 * Nama hari "Senin" .. "Minggu" untuk header kolom kalender.
 * Input adalah Date UTC; output selalu nama hari di zona Asia/Jakarta.
 */
export function jakartaDayName(value: string | Date): string {
  return ISO_DAY_NAMES_LONG[isoWeekdayJakarta(value)];
}

/**
 * Kunci tanggal "YYYY-MM-DD" di zona Asia/Jakarta — dipakai sebagai key
 * map saat mengelompokkan slot per hari.
 *
 * Penting: jangan pakai `value.toISOString().slice(0, 10)` — itu UTC dan
 * bisa salah tanggal di sekitar tengah malam WIB.
 */
export function jakartaDateKey(value: string | Date): string {
  return formatInTimeZone(parseRFC3339(value), TIMEZONE, "yyyy-MM-dd");
}

/**
 * Cek apakah dua tanggal berada di hari yang SAMA menurut zona Asia/Jakarta.
 *
 * PENTING: Bandingkan dua Instant terhadap kalender WIB, BUKAN komponen
 * tanggal UTC. Ini adalah operasi KONSEP KALENDER, bukan perbandingan epoch.
 *
 * @param a Tanggal pertama (RFC3339 atau Date)
 * @param b Tanggal kedua (RFC3339 atau Date)
 * @returns true kalau keduanya jatuh di tanggal WIB yang sama
 *
 * Contoh:
 * - "2026-09-10T17:00:00Z" (11 Sep 00:00 WIB) dan "2026-09-11T09:00:00Z" (11 Sep 09:00 WIB) → true
 * - "2026-09-10T16:00:00Z" (10 Sep 23:00 WIB) dan "2026-09-11T01:00:00Z" (11 Sep 08:00 WIB) → false
 */
export function isSameJakartaDay(a: string | Date, b: string | Date): boolean {
  return jakartaDateKey(a) === jakartaDateKey(b);
}
export function startOfWeekJakarta(reference: string | Date = new Date()): Date {
  // Strategi: bangun wall-time Senin di zona Jakarta sebagai string ISO,
  // lalu konversi ke Date UTC via fromZonedTime. Hasilnya selalu titik
  // waktu 00:00 WIB hari Senin yang dimaksud — tanpa peduli zona lokal.
  const dateKey = jakartaDateKey(reference); // YYYY-MM-DD di Jakarta
  const isoDay = isoWeekdayJakarta(reference); // 1=Senin .. 7=Minggu
  const [year, month, day] = dateKey.split("-").map(Number);
  // Kurangi `isoDay - 1` hari dari `day` (Senin = 0 offset).
  const refUTC = new Date(Date.UTC(year, month - 1, day));
  refUTC.setUTCDate(refUTC.getUTCDate() - (isoDay - 1));
  const mondayWallISO =
    `${refUTC.getUTCFullYear()}-` +
    `${String(refUTC.getUTCMonth() + 1).padStart(2, "0")}-` +
    `${String(refUTC.getUTCDate()).padStart(2, "0")}T00:00:00`;
  return fromZonedTime(mondayWallISO, TIMEZONE);
}

/**
 * Bangun rentang minggu [from, to) di zona Asia/Jakarta.
 * `to` adalah Senin minggu BERIKUTNYA (exclusive) sehingga loop harian
 * `for (let d = from; d < to; d = +1 hari)` berhenti sendiri.
 *
 * Keduanya adalah Date UTC yang merepresentasikan Senin 00:00 WIB.
 */
export function weekRangeJakarta(reference: string | Date = new Date()): {
  from: Date;
  to: Date;
} {
  const monday = startOfWeekJakarta(reference);
  const to = new Date(monday.getTime() + 7 * 24 * 60 * 60 * 1000);
  return { from: monday, to };
}

/**
 * Label minggu kalender: "7–13 September 2026" (Senin..Minggu bulan sama).
 * Dipakai untuk header kalender.
 */
export function formatWeekRangeLabel(reference: string | Date = new Date()): string {
  const monday = startOfWeekJakarta(reference);
  const sunday = new Date(monday.getTime() + 6 * 24 * 60 * 60 * 1000);
  // Pakai formatInTimeZone untuk kedua titik supaya konsisten zona Jakarta.
  const mondayMonth = formatInTimeZone(monday, TIMEZONE, "M");
  const sundayMonth = formatInTimeZone(sunday, TIMEZONE, "M");
  const mondayYear = formatInTimeZone(monday, TIMEZONE, "yyyy");
  const sundayYear = formatInTimeZone(sunday, TIMEZONE, "yyyy");

  if (mondayMonth === sundayMonth && mondayYear === sundayYear) {
    return `${formatInTimeZone(monday, TIMEZONE, "d")}–${formatInTimeZone(
      sunday,
      TIMEZONE,
      "d MMMM yyyy",
      { locale: localeId }
    )}`;
  }
  if (mondayYear === sundayYear) {
    return `${formatInTimeZone(monday, TIMEZONE, "d MMMM", {
      locale: localeId,
    })} – ${formatInTimeZone(sunday, TIMEZONE, "d MMMM yyyy", {
      locale: localeId,
    })}`;
  }
  return `${formatInTimeZone(monday, TIMEZONE, "d MMMM yyyy", {
    locale: localeId,
  })} – ${formatInTimeZone(sunday, TIMEZONE, "d MMMM yyyy", {
    locale: localeId,
  })}`;
}

/**
 * Cek apakah `reference` ada di minggu yang LEBIH AWAL dari minggu ini.
 * Dipakai untuk menonaktifkan tombol "prev" bila minggu sebelumnya seluruhnya
 * sudah lewat dari "hari ini" Asia/Jakarta.
 */
export function isBeforeThisWeekJakarta(reference: string | Date): boolean {
  const thisMonday = startOfWeekJakarta();
  const candidate = startOfWeekJakarta(reference);
  return candidate.getTime() < thisMonday.getTime();
}

/**
 * Kelompokkan daftar slot ke peta per tanggal Asia/Jakarta.
 * Backend mengirim slot dalam UTC — kalau dikelompokkan dengan
 * `slot.start_at.slice(0,10)` kita akan salah satu hari di sekitar
 * tengah malam WIB. Lewat jakartaDateKey() supaya konsisten dengan
 * kalender yang ditampilkan.
 */
export function groupSlotsByJakartaDate<T extends { start_at: string }>(
  slots: T[]
): Map<string, T[]> {
  const out = new Map<string, T[]>();
  for (const s of slots) {
    const key = jakartaDateKey(s.start_at);
    const list = out.get(key);
    if (list) list.push(s);
    else out.set(key, [s]);
  }
  return out;
}

/**
 * Daftar label hari kolom kalender (Senin..Minggu) untuk minggu `reference`.
 * `date` adalah Date UTC yang merepresentasikan tengah malam WIB hari itu.
 */
export function weekDayHeaders(reference: string | Date = new Date()): {
  key: string;
  label: string;
  date: Date;
}[] {
  const monday = startOfWeekJakarta(reference);
  const out: { key: string; label: string; date: Date }[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(monday.getTime() + i * 24 * 60 * 60 * 1000);
    out.push({
      key: jakartaDateKey(d),
      label: ISO_DAY_NAMES_LONG[isoWeekdayJakarta(d)],
      date: d,
    });
  }
  return out;
}

/**
 * Daftar hari dalam minggu `reference` sebagai Date UTC (midnight WIB).
 * Dipakai untuk iterasi sel dalam grid kalender (termasuk hari tanpa slot).
 */
export function weekDaysJakarta(reference: string | Date = new Date()): Date[] {
  const monday = startOfWeekJakarta(reference);
  const out: Date[] = [];
  for (let i = 0; i < 7; i++) {
    out.push(new Date(monday.getTime() + i * 24 * 60 * 60 * 1000));
  }
  return out;
}
