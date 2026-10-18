/**
 * Test lib/date.ts: fokus pada konversi zona Asia/Jakarta.
 *
 * Paling penting: lintas batas hari UTC. Slot 17:00 UTC harus tampil
 * 00:00 WIB keesokan harinya. Kelas bug ini yang terjadi di backend M3b
 * (jam dev WITA vs kampus WIB) dan tidak boleh masuk ke frontend.
 */

import { describe, expect, it } from "vitest";
import {
  formatJakartaTime,
  formatJakartaLongDate,
  formatJakartaShortDate,
  formatJakartaDate,
  jakartaDateKey,
  jakartaDayName,
  startOfWeekJakarta,
  weekRangeJakarta,
  isBeforeThisWeekJakarta,
  groupSlotsByJakartaDate,
  weekDayHeaders,
  weekDaysJakarta,
  formatWeekRangeLabel,
  formatTimeRange,
  formatWallClockTimeRange,
  formatDateRange,
  isSameJakartaDay,
} from "@/lib/date";

describe("lib/date: konversi zona Asia/Jakarta", () => {
  it("17:00 UTC = 00:00 WIB keesokan harinya (batas hari)", () => {
    // 2026-09-10T17:00:00Z = 2026-09-11T00:00:00+07:00
    const utc = "2026-09-10T17:00:00Z";

    expect(formatJakartaTime(utc)).toBe("00:00");
    expect(jakartaDateKey(utc)).toBe("2026-09-11");
    expect(jakartaDayName(utc)).toBe("Jumat");
  });

  it("16:59 UTC masih hari yang sama di WIB", () => {
    // 2026-09-10T16:59:00Z = 2026-09-10T23:59:00+07:00
    const utc = "2026-09-10T16:59:00Z";

    expect(formatJakartaTime(utc)).toBe("23:59");
    expect(jakartaDateKey(utc)).toBe("2026-09-10");
  });

  it("jakartaDayName konsisten ISO weekday (Senin..Minggu)", () => {
    // 2026-09-07 adalah Senin (kalender tetap, tidak tergantung zona).
    // Untuk name kita verifikasi Lewat zona: pakai 04:00 WIB yang berarti
    // 2026-09-06T21:00:00Z di UTC; ini tetap tanggal Senin di WIB.
    expect(jakartaDayName("2026-09-07T04:00:00+07:00")).toBe("Senin");
    expect(jakartaDayName("2026-09-08T04:00:00+07:00")).toBe("Selasa");
    expect(jakartaDayName("2026-09-12T04:00:00+07:00")).toBe("Sabtu");
    expect(jakartaDayName("2026-09-13T04:00:00+07:00")).toBe("Minggu");
  });

  it("startOfWeekJakarta kembali ke Senin minggu yang sama", () => {
    // Kamis 10 Sep 2026 -> Senin 7 Sep 2026.
    const kamis = new Date("2026-09-10T08:00:00+07:00");
    const senin = startOfWeekJakarta(kamis);
    // getDay() ikut zona proses, bukan WIB (Senin 00:00 WIB = Minggu 17:00
    // UTC), jadi hari dicek lewat helper yang memang pakai Asia/Jakarta.
    expect(jakartaDayName(senin)).toBe("Senin");
    expect(jakartaDateKey(senin)).toBe("2026-09-07");
  });

  it("startOfWeekJakarta untuk hari Minggu kembali ke Senin minggu ITU", () => {
    // Minggu 13 Sep 2026 -> Senin 7 Sep 2026 (minggu yang sama, ISO).
    const minggu = new Date("2026-09-13T08:00:00+07:00");
    const senin = startOfWeekJakarta(minggu);
    expect(jakartaDayName(senin)).toBe("Senin");
    expect(jakartaDateKey(senin)).toBe("2026-09-07");
  });

  it("weekRangeJakarta mengembalikan from/to UTC dengan to = Senin berikutnya", () => {
    // Acuan: Kamis 10 Sep 2026.
    const { from, to } = weekRangeJakarta("2026-09-10T08:00:00+07:00");

    // from adalah Senin 00:00 WIB = 16:00 UTC hari sebelumnya.
    expect(from.toISOString()).toBe("2026-09-06T17:00:00.000Z");
    // to adalah Senin berikutnya 00:00 WIB.
    expect(to.toISOString()).toBe("2026-09-13T17:00:00.000Z");
  });

  it("groupSlotsByJakartaDate mengelompokkan slot sesuai tanggal WIB", () => {
    const slots = [
      { id: "s1", start_at: "2026-09-10T17:00:00Z" }, // 11 Sep 00:00 WIB
      { id: "s2", start_at: "2026-09-11T02:00:00Z" }, // 11 Sep 09:00 WIB
      { id: "s3", start_at: "2026-09-11T10:00:00Z" }, // 11 Sep 17:00 WIB
      { id: "s4", start_at: "2026-09-12T01:00:00Z" }, // 12 Sep 08:00 WIB
    ];

    const grouped = groupSlotsByJakartaDate(slots);

    expect(grouped.size).toBe(2);
    expect(grouped.get("2026-09-11")?.map((s) => s.id)).toEqual(["s1", "s2", "s3"]);
    expect(grouped.get("2026-09-12")?.map((s) => s.id)).toEqual(["s4"]);
  });

  it("weekDayHeaders menghasilkan 7 hari Senin..Minggu untuk minggu acuan", () => {
    const headers = weekDayHeaders("2026-09-10T08:00:00+07:00");
    expect(headers).toHaveLength(7);
    expect(headers.map((h) => h.label)).toEqual([
      "Senin",
      "Selasa",
      "Rabu",
      "Kamis",
      "Jumat",
      "Sabtu",
      "Minggu",
    ]);
    expect(headers[0].key).toBe("2026-09-07");
    expect(headers[6].key).toBe("2026-09-13");
  });

  it("weekDaysJakarta menghasilkan 7 hari berturut-turut", () => {
    const days = weekDaysJakarta("2026-09-10T08:00:00+07:00");
    expect(days).toHaveLength(7);
    expect(jakartaDateKey(days[0])).toBe("2026-09-07");
    expect(jakartaDateKey(days[6])).toBe("2026-09-13");
  });

  it("isBeforeThisWeekJakarta: minggu lalu = true, minggu ini = false", () => {
    // Pakai Senin minggu lalu sebagai acuan: harusnya sebelum minggu ini.
    const mingguLalu = new Date();
    mingguLalu.setDate(mingguLalu.getDate() - 14);
    expect(isBeforeThisWeekJakarta(mingguLalu)).toBe(true);

    // Senin minggu ini (asumsi tes dijalankan sekitar tanggal saat ini)
    // Ambil Senin minggu ini dari `new Date()` lewat startOfWeekJakarta;
    // seharusnya bukan sebelum minggu ini.
    const mingguIni = startOfWeekJakarta();
    expect(isBeforeThisWeekJakarta(mingguIni)).toBe(false);
  });

  it("formatWeekRangeLabel menulis rentang dalam Bahasa Indonesia", () => {
    // 7 - 13 Sep 2026 (Senin..Minggu bulan sama).
    expect(formatWeekRangeLabel("2026-09-10T08:00:00+07:00")).toBe(
      "7 - 13 Sep 2026"
    );
  });

  it("formatTimeRange menulis 'HH:mm - HH:mm' di zona Jakarta", () => {
    // 09.00 - 09.30 WIB.
    expect(
      formatTimeRange("2026-09-11T09:00:00+07:00", "2026-09-11T09:30:00+07:00")
    ).toBe("09:00 - 09:30");
  });

  it("formatTimeRange menghormati zona Jakarta saat UTC beda hari", () => {
    // 17:00 UTC tanggal 10 -> 00:00 WIB tanggal 11.
    // 18:00 UTC tanggal 10 -> 01:00 WIB tanggal 11.
    expect(
      formatTimeRange("2026-09-10T17:00:00Z", "2026-09-10T18:00:00Z")
    ).toBe("00:00 - 01:00");
  });

  it("formatWallClockTimeRange menulis 'HH:MM - HH:MM' dari string jam", () => {
    // Aturan ketersediaan: start_time/end_time adalah string "HH:MM",
    // bukan Instant RFC3339. Tetapkan format konsisten dengan
    // formatTimeRange: pakai RANGE_SEPARATOR ASCII.
    expect(formatWallClockTimeRange("09:00", "12:30")).toBe("09:00 - 12:30");
    expect(formatWallClockTimeRange("00:00", "23:59")).toBe("00:00 - 23:59");
  });

  it("formatWallClockTimeRange membuang detik kalau string membawa 'HH:MM:SS'", () => {
    // Input type="time" di DOM dengan step < 60 bisa menghasilkan
    // "HH:MM:SS". Tampilan harusnya tetap "HH:MM" (detik tidak
    // relevan untuk jadwal konsultasi), dan RANGE_SEPARATOR tetap.
    expect(formatWallClockTimeRange("09:00:00", "12:30:45")).toBe(
      "09:00 - 12:30"
    );
  });

  it("formatDateRange bulan sama -> 'd - d MMM yyyy'", () => {
    // 7 - 13 Sep 2026 (Senin..Minggu bulan sama, tahun sama).
    expect(
      formatDateRange("2026-09-07T00:00:00+07:00", "2026-09-13T00:00:00+07:00")
    ).toBe("7 - 13 Sep 2026");
  });

  it("formatDateRange bulan beda, tahun sama -> 'd MMM - d MMM yyyy'", () => {
    // 28 Okt - 3 Nov 2026.
    expect(
      formatDateRange("2026-10-28T00:00:00+07:00", "2026-11-03T00:00:00+07:00")
    ).toBe("28 Okt - 3 Nov 2026");
  });

  it("formatDateRange tahun beda -> 'd MMM yyyy - d MMM yyyy'", () => {
    // 28 Des 2026 - 3 Jan 2027.
    expect(
      formatDateRange("2026-12-28T00:00:00+07:00", "2027-01-03T00:00:00+07:00")
    ).toBe("28 Des 2026 - 3 Jan 2027");
  });

  it("formatDateRange mengikuti kalender WIB walau UTC beda tanggal", () => {
    // Senin 7 Sep 2026 00:00 WIB = 2026-09-06T17:00:00Z.
    // Minggu 13 Sep 2026 00:00 WIB = 2026-09-12T17:00:00Z.
    // Kalau pakai UTC, tanggal jadi 6 - 12 September, bukan 7 - 13.
    expect(
      formatDateRange("2026-09-06T17:00:00Z", "2026-09-12T17:00:00Z")
    ).toBe("7 - 13 Sep 2026");
  });

  it("formatDateRange Agustus memakai singkatan date-fns id ('Agt')", () => {
    // 3 - 9 Agt 2026 (Senin..Minggu bulan sama, Agustus).
    // Asal singkatan: locale 'id' dari date-fns, lihat
    // web/node_modules/date-fns/locale/id/_lib/localize.js (Agustus -> "Agt").
    // 'Agt' di sini konsisten dengan formatJakartaShortDate untuk Agustus
    // (juga "10 Agt"), sehingga label minggu tidak berbeda gaya dengan
    // header kolom kalender.
    expect(
      formatDateRange("2026-08-03T00:00:00+07:00", "2026-08-09T00:00:00+07:00")
    ).toBe("3 - 9 Agt 2026");
  });

  it("formatDateRange Agustus beda bulan -> 'Agt' dan 'Sep'", () => {
    // 27 Agt - 2 Sep 2026 (Minggu sebelumnya - Senin berikutnya).
    expect(
      formatDateRange("2026-08-27T00:00:00+07:00", "2026-09-02T00:00:00+07:00")
    ).toBe("27 Agt - 2 Sep 2026");
  });

  it("formatDateRange start = end menulis satu tanggal saja", () => {
    // Tanggal awal dan akhir jatuh di hari Jakarta yang sama: hasilnya
    // satu tanggal tanpa RANGE_SEPARATOR, bukan "7 - 7 Sep 2026".
    expect(
      formatDateRange("2026-09-07T00:00:00+07:00", "2026-09-07T00:00:00+07:00")
    ).toBe("7 Sep 2026");
  });

  it("formatDateRange hari Jakarta sama walau UTC beda -> satu tanggal", () => {
    // 2026-09-06T17:00:00Z = 7 Sep 00:00 WIB
    // 2026-09-07T10:00:00Z = 7 Sep 17:00 WIB
    // Keduanya di hari yang sama di kalender WIB.
    expect(
      formatDateRange("2026-09-06T17:00:00Z", "2026-09-07T10:00:00Z")
    ).toBe("7 Sep 2026");
  });

  it("formatJakartaLongDate menggunakan locale Indonesia", () => {
    // 2026-09-11T01:00:00+07:00 = 2026-09-10T18:00:00Z.
    expect(formatJakartaLongDate("2026-09-10T18:00:00Z")).toBe(
      "Jumat, 11 September 2026"
    );
  });

  it("formatJakartaShortDate menulis 'd MMM' sesuai zona Jakarta", () => {
    // 10 September 2026 WIB.
    expect(formatJakartaShortDate("2026-09-10T01:00:00Z")).toBe("10 Sep");
    // Batas hari: 17:00 UTC tanggal 10 -> 00:00 tanggal 11 WIB.
    expect(formatJakartaShortDate("2026-09-10T17:00:00Z")).toBe("11 Sep");
  });

  it("formatJakartaDate menulis 'd MMM yyyy' sesuai zona Jakarta", () => {
    // 14 September 2026 WIB.
    expect(formatJakartaDate("2026-09-14T00:00:00Z")).toBe("14 Sep 2026");
    // 14 September 2026 09:00 WIB = 2026-09-14T02:00:00Z.
    expect(formatJakartaDate("2026-09-14T02:00:00Z")).toBe("14 Sep 2026");
    // Agustus memakai "Agt" (date-fns id), konsisten dengan
    // formatDateRange dan formatJakartaShortDate.
    expect(formatJakartaDate("2026-08-10T00:00:00Z")).toBe("10 Agt 2026");
  });
});

describe("isSameJakartaDay", () => {
  it("17:00 UTC dan 18:00 UTC tanggal sama di WIB -> true", () => {
    // 2026-09-10T17:00:00Z = 2026-09-11T00:00+07:00
    // 2026-09-10T18:00:00Z = 2026-09-11T01:00+07:00
    // Keduanya di tanggal 11 September WIB
    expect(isSameJakartaDay("2026-09-10T17:00:00Z", "2026-09-10T18:00:00Z")).toBe(true);
  });

  it("16:59 UTC dan 17:00 UTC beda hari di WIB -> false", () => {
    // 2026-09-10T16:59:00Z = 2026-09-10T23:59+07:00
    // 2026-09-10T17:00:00Z = 2026-09-11T00:00+07:00
    expect(isSameJakartaDay("2026-09-10T16:59:00Z", "2026-09-10T17:00:00Z")).toBe(false);
  });

  it("tanggal sama dengan offset berbeda -> true", () => {
    expect(isSameJakartaDay("2026-09-11T00:00:00+07:00", "2026-09-10T17:00:00Z")).toBe(true);
    expect(isSameJakartaDay("2026-09-11T12:00:00+07:00", "2026-09-11T05:00:00Z")).toBe(true);
  });

  it("Date object vs string -> tetap bekerja", () => {
    const date = new Date("2026-09-11T00:00:00+07:00");
    expect(isSameJakartaDay(date, "2026-09-10T17:00:00Z")).toBe(true);
  });
});
