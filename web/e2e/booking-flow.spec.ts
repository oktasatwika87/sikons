import { expect, test } from "@playwright/test";
import { id as localeId } from "date-fns/locale";
import { formatInTimeZone } from "date-fns-tz";

/**
 * Skenario E2E SIKONS: dua peran dalam satu spec mode serial.
 *
 * Test 1: Mahasiswa:
 *   daftar -> login -> reload halaman (harus tetap login) -> cari dosen
 *   demo dari seed -> pilih slot tersedia -> booking -> verifikasi di
 *   /konsultasi.
 *
 * Test 2: Dosen (akun seed):
 *   login -> booking dari test 1 muncul di /dashboard -> kalau backend
 *   mengizinkan (sesi sudah dimulai) baru coba "Tandai Selesai" + isi
 *   catatan. Kalau tidak, cukup verifikasi booking muncul. Tidak
 *   mengakali jam.
 *
 * Aturan ketat (instruksi M7b-1):
 * - Hanya chromium. workers 1, fullyParallel false (di config).
 * - Slot dipilih lewat API (GET /lecturers/{id}/slots) untuk dapat yang
 *   > 60 menit dari sekarang (BOOKING_MIN_LEAD_MINUTES default). UI
 *   tetap yang meng-klik slot, submit, dan menampilkan hasil.
 * - Geser minggu maksimal 5 kali kalau minggu berjalan tidak punya slot
 *   valid.
 * - data-testid TIDAK dipakai: semua selector pakai getByRole /
 *   getByLabel / getByText.
 */

const API_BASE = "http://localhost:8080/api/v1";
const TIMEZONE = "Asia/Jakarta";

// Kredensial dosen demo: dibaca dari variabel lingkungan.
// Override lewat variabel lingkungan kalau seed berubah.
const DOSEN_EMAIL = process.env.E2E_DOSEN_EMAIL ?? "budi@sikons.test";
const DOSEN_PASSWORD = process.env.E2E_DOSEN_PASSWORD ?? "password123";
// Nama tampil di daftar dosen /dosen. Pakai untuk getByRole('link').
const DOSEN_NAME = process.env.E2E_DOSEN_NAME ?? "Dr. Budi Santoso";

// Batas horizon endpoint GET /lecturers/{id}/slots adalah 30 hari
// (s.slotHorizon dari SLOT_HORIZON_DAYS, default 30). Pakai 28 hari supaya
// aman di bawah batas dan tidak kena 422.
const SLOT_HORIZON_DAYS = 28;

test.describe.serial("alur konsultasi E2E", () => {
  // State bersama yang dipakai test 2.
  const state: {
    studentEmail?: string;
    studentFullName?: string;
    studentNIM?: string;
    bookingTopic?: string;
  } = {};

  test("mahasiswa: daftar, login persisten, booking slot, muncul di /konsultasi", async ({
    page,
    request,
  }) => {
    // 1. Daftar dengan email unik per run.
    const runId = `${Date.now()}-${Math.floor(Math.random() * 100_000)}`;
    state.studentEmail = `e2e-${runId}@sikons.test`;
    state.studentFullName = `Mahasiswa E2E ${runId}`;
    state.studentNIM = `E2E${runId.slice(-8)}`;
    state.bookingTopic = `Konsultasi E2E ${runId}`;
    const password = "PasswordE2E123!";

    await page.goto("/register");
    await expect(page.getByRole("heading", { name: "Daftar" })).toBeVisible();
    await page.getByLabel("Nama lengkap").fill(state.studentFullName);
    await page.getByLabel("Nomor identitas (NIM/NIP)").fill(state.studentNIM);
    await page.getByLabel("Email").fill(state.studentEmail);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByLabel("Konfirmasi password").fill(password);
    await page.getByRole("button", { name: "Daftar", exact: true }).click();

    // Register -> redirect ke /login?registered=1 (TIDAK auto-login).
    await page.waitForURL(/\/login/);
    await expect(page.getByText("Registrasi berhasil, silakan masuk.")).toBeVisible();

    // 2. Login dengan akun yang baru dibuat.
    await page.getByLabel("Email").fill(state.studentEmail);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Masuk", exact: true }).click();
    await page.waitForURL(/\/$/);

    // 3. Reload halaman: harus TETAP login. Ini yang dites mutation
    //    kalau POST /auth/refresh dihapus dari bootstrap sesi.
    //    JANGAN pakai page.waitForURL(/\/dosen/): URL sudah cocok
    //    sebelum reload karena /  langsung redirect ke /dosen lewat
    //    RoleRedirect saat auth.authenticated. Yang kita tunggu
    //    adalah konfirmasi bahwa bootstrap sesi SELESAI: yaitu
    //    Nav menampilkan nama user (elemen yang hanya muncul kalau
    //    auth.status === "authenticated" && auth.user). Tanpa ini,
    //    klik slot di langkah 7 bisa kena 401 karena access token
    //    belum tersedia di memory.
    await page.reload();
    await expect(page.getByText(state.studentFullName)).toBeVisible({
      timeout: 10_000,
    });

    // 4. Cari dosen demo dari seed di /dosen. Halaman saat ini adalah
    //    /dosen (hasil redirect dari /). Pakai kolom pencarian supaya
    //    tidak bergantung pada pagination dari entri "Dosen Test"
    //    yang dibuat test lain sebelumnya.
    const searchInput = page.getByLabel("Nama");
    await expect(searchInput).toBeVisible();
    await searchInput.fill("Budi");
    // Debounce input 350ms + fetch list dengan filter.
    const dosenLink = page.getByRole("link", { name: new RegExp(DOSEN_NAME) }).first();
    await dosenLink.waitFor({ state: "visible", timeout: 10_000 });
    await dosenLink.click();
    await page.waitForURL(/\/dosen\/[a-f0-9-]+/);

    // Ambil lecturerId dari URL untuk query API.
    const url = new URL(page.url());
    const lecturerId = url.pathname.split("/").pop()!;

    // 5. Cari slot valid lewat API publik (start_at > now + 60 menit,
    //    status open). Tampilan UI dipakai untuk klik & submit.
    const horizon = new Date(Date.now() + SLOT_HORIZON_DAYS * 24 * 60 * 60 * 1000);
    const apiResp = await request.get(
      `${API_BASE}/lecturers/${lecturerId}/slots?from=${encodeURIComponent(
        new Date().toISOString()
      )}&to=${encodeURIComponent(horizon.toISOString())}`
    );
    expect(apiResp.ok(), `API slots status ${apiResp.status()}`).toBeTruthy();
    const body = (await apiResp.json()) as {
      slots: Array<{ id: string; start_at: string; end_at: string; status: string }>;
    };
    const minLeadMs = Date.now() + 60 * 60 * 1000;
    const validSlot = body.slots.find(
      (s) => s.status === "open" && new Date(s.start_at).getTime() > minLeadMs
    );
    if (!validSlot) {
      throw new Error(
        `tidak ada slot open > 60 menit dalam ${SLOT_HORIZON_DAYS} hari ke depan. ` +
          `Pastikan sudah menjalankan 'make seed' dan 'make generate-slots'.`
      );
    }

    // 6. Geser minggu sampai muncul kolom untuk hari slot.
    const slotStart = new Date(validSlot.start_at);
    const weeksToAdvance = weekIndexJakarta(slotStart) - weekIndexJakarta(new Date());
    expect(
      weeksToAdvance,
      "slot valid harus dalam horizon ke depan"
    ).toBeGreaterThanOrEqual(0);
    // Batas minggu yang diizinkan: horizon 28 hari / 7 = 4 minggu.
    const maxWeeks = Math.ceil(SLOT_HORIZON_DAYS / 7);
    expect(weeksToAdvance, "slot valid harus dalam " + maxWeeks + " minggu ke depan").toBeLessThan(
      maxWeeks
    );
    for (let i = 0; i < weeksToAdvance; i++) {
      await page.getByRole("button", { name: "Berikutnya" }).click();
      // Tunggu sampai label minggu berubah.
      await page.waitForTimeout(500);
    }

    // 7. Klik slot yang sesuai. Cari di kolom hari yang benar (label
    //    tanggal "d MMM" di zona Jakarta, locale id -> "12 Okt"), lalu
    //    tombol dengan time range.
    const dayLabel = formatInTimeZone(slotStart, TIMEZONE, "d MMM", {
      locale: localeId,
    });
    const startText = formatInTimeZone(slotStart, TIMEZONE, "HH:mm");
    const endText = formatInTimeZone(new Date(validSlot.end_at), TIMEZONE, "HH:mm");
    // Pemisah universal RANGE_SEPARATOR dari lib/date: " - " (ASCII).
    const timeRange = `${startText} - ${endText}`;

    const dayDateEl = page.getByText(dayLabel, { exact: true }).first();
    await expect(dayDateEl, `header tanggal "${dayLabel}" minggu ini`).toBeVisible();
    // Naik ke kontainer kolom (div dengan class rounded-lg), lalu cari tombol.
    const column = dayDateEl.locator(
      'xpath=ancestor::div[contains(@class, "rounded-lg")][1]'
    );
    const slotButton = column.getByRole("button", { name: timeRange, exact: true });
    await expect(slotButton, `slot ${timeRange} pada ${dayLabel}`).toBeVisible();
    await slotButton.click();

    // 8. Dialog booking muncul. Isi topik, submit.
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("Topik").fill(state.bookingTopic);
    await dialog.getByRole("button", { name: "Pesan", exact: true }).click();

    // Tunggu dialog tertutup -> booking sukses.
    await expect(dialog).toBeHidden({ timeout: 15_000 });

    // 9. Buka /konsultasi, verifikasi booking ada.
    await page.goto("/konsultasi");
    await expect(page.getByRole("heading", { name: "Riwayat Konsultasi" })).toBeVisible();
    // Verifikasi topik muncul di tabel (desktop view): selector eksplisit
    // supaya tidak ambigu dengan mobile card yang md:hidden.
    await expect(
      page.locator("table").getByText(state.bookingTopic).first()
    ).toBeVisible({ timeout: 10_000 });
    // Status "Akan Datang" tampil di badge tabel.
    await expect(
      page.locator("table").getByText("Akan Datang").first()
    ).toBeVisible();
  });

  test("dosen: login, booking mahasiswa muncul di dashboard, dan (kalau sesi sudah mulai) tandai selesai + isi catatan", async ({
    page,
  }) => {
    // 1. Login sebagai dosen demo.
    await page.goto("/login");
    await page.getByLabel("Email").fill(DOSEN_EMAIL);
    await page.getByLabel("Password").fill(DOSEN_PASSWORD);
    await page.getByRole("button", { name: "Masuk", exact: true }).click();
    await page.waitForURL(/\/dashboard/);
    await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

    // 2. Verifikasi booking dari test sebelumnya muncul.
    const topicText = page.getByText(state.bookingTopic!).first();
    await expect(topicText, "booking mahasiswa harus muncul di dashboard").toBeVisible({
      timeout: 10_000,
    });

    // 3. Kalau ada tombol "Tandai Selesai" (artinya sesi sudah dimulai),
    //    buka dialog, isi catatan, simpan. Kalau tidak ada, jangan apa-apa
    //   : cukup verifikasi booking muncul (sesuai instruksi: jangan
    //    mengakali jam).
    const completeButton = page
      .getByRole("button", { name: "Tandai Selesai", exact: true })
      .first();
    const completeVisible = await completeButton.isVisible().catch(() => false);
    if (!completeVisible) {
      test.info().annotations.push({
        type: "skip-complete",
        description:
          "Sesi belum dimulai (tombol Tandai Selesai tidak ada). " +
          "Cukup verifikasi booking muncul: backend tidak mengizinkan complete sebelum jam sesi.",
      });
      return;
    }

    await completeButton.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading", { name: "Tandai Selesai" })).toBeVisible();
    const catatan = `Catatan E2E otomatis untuk ${state.bookingTopic}`;
    await dialog.getByLabel("Catatan (opsional)").fill(catatan);
    await dialog.getByRole("button", { name: "Simpan", exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 10_000 });
  });
});

// ------------------------------------------------------------------ helper

/**
 * Indeks minggu ISO untuk tanggal tertentu di zona Asia/Jakarta.
 * Senin = awal minggu. Dipakai untuk menghitung berapa kali tombol
 * "Berikutnya" harus ditekan sebelum kolom hari slot muncul.
 */
function weekIndexJakarta(date: Date): number {
  // Hari ISO (1=Senin, 7=Minggu) di zona Jakarta.
  const isoDay = parseInt(formatInTimeZone(date, TIMEZONE, "i"), 10);
  // Tanggal YYYY-MM-DD di zona Jakarta.
  const dateKey = formatInTimeZone(date, TIMEZONE, "yyyy-MM-dd");
  const [y, m, d] = dateKey.split("-").map(Number);
  // Hitung hari sejak epoch (UTC midnight): cukup untuk indeks minggu.
  const utcMillis = Date.UTC(y, m - 1, d);
  const daysSinceEpoch = Math.floor(utcMillis / (24 * 60 * 60 * 1000));
  return Math.floor((daysSinceEpoch - (isoDay - 1)) / 7);
}
