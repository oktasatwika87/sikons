/**
 * Screenshot test untuk verifikasi layout responsive.
 *
 * Pakai: npx tsx e2e/responsive-screenshots.ts
 * Butuh: next dev (port 3000) sedang berjalan.
 *
 * Menghasilkan screenshot ke /tmp/sikons-screenshots/ untuk verify:
 * - Tidak ada horizontal scroll
 * - Layout berubah (tabel → kartu di mobile)
 * - Elemen penting hadir (tombol Batalkan, dll.)
 *
 * Jalankan saat setup awal atau saat ada perubahan CSS besar.
 */
import { chromium } from "@playwright/test";

const BASE_URL = "http://localhost:3000";
const OUTPUT_DIR = "/tmp/sikons-screenshots";

// Viewports yang diuji
const VIEWPORTS = [
  { name: "mobile", width: 375, height: 667 },
  { name: "tablet", width: 768, height: 1024 },
];

// Halaman yang diuji
const PAGES = [
  { name: "konsultasi", path: "/konsultasi", needsAuth: true },
  { name: "dosen-detail", path: "/dosen/1", needsAuth: false },
  { name: "ketersediaan", path: "/ketersediaan", needsAuth: true },
];

async function loginAsStudent(page: any) {
  // Intercept /auth/me untuk mock user
  await page.route("**/api/v1/auth/me", route => {
    route.fulfill({
      json: {
        user: {
          id: "student-1",
          email: "budi@ui.ac.id",
          full_name: "Budi Santoso",
          role: "student",
          is_active: true,
          created_at: new Date().toISOString(),
        },
      },
      headers: { "Content-Type": "application/json" },
    });
  });

  // Intercept /bookings
  await page.route("**/api/v1/bookings**", route => {
    route.fulfill({
      json: {
        bookings: [
          {
            id: "booking-1",
            status: "confirmed",
            topic: "Bimbingan Skripsi",
            description: "Diskusi progress skripsi",
            created_at: new Date().toISOString(),
            lecturer_note: null,
            slot: {
              id: "slot-1",
              start_at: new Date(Date.now() + 2 * 24 * 60 * 60 * 1000).toISOString(),
              end_at: new Date(Date.now() + 2 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
            },
            lecturer: {
              id: "lecturer-1",
              full_name: "Dr. Ahmad Wijaya",
              department: "Informatika",
            },
          },
        ],
        page: 1,
        per_page: 20,
        total: 1,
      },
      headers: { "Content-Type": "application/json" },
    });
  });
}

async function loginAsLecturer(page: any) {
  // Intercept /auth/me untuk mock user
  await page.route("**/api/v1/auth/me", route => {
    route.fulfill({
      json: {
        user: {
          id: "lecturer-1",
          email: "ahmad@ui.ac.id",
          full_name: "Dr. Ahmad Wijaya",
          role: "lecturer",
          is_active: true,
          created_at: new Date().toISOString(),
        },
      },
      headers: { "Content-Type": "application/json" },
    });
  });

  // Intercept /availability
  await page.route("**/api/v1/availability**", route => {
    route.fulfill({
      json: { rules: [], exceptions: [] },
      headers: { "Content-Type": "application/json" },
    });
  });
}

async function run() {
  const browser = await chromium.launch({ headless: true });

  for (const vp of VIEWPORTS) {
    console.log(`\n=== Viewport: ${vp.name} (${vp.width}x${vp.height}) ===`);

    // Test sebagai student
    const studentContext = await browser.newContext({
      viewport: { width: vp.width, height: vp.height },
    });
    const studentPage = await studentContext.newPage();
    await loginAsStudent(studentPage);

    for (const pg of PAGES) {
      if (!pg.needsAuth) continue;

      console.log(`  [student] ${pg.name}...`);
      try {
        await studentPage.goto(`${BASE_URL}${pg.path}`, { waitUntil: "networkidle", timeout: 30000 });
        await studentPage.waitForTimeout(1000);

        const filename = `${OUTPUT_DIR}/${pg.name}-${vp.name}-student.png`;
        await studentPage.screenshot({ path: filename, fullPage: false });

        // Get page info
        const hasHorizontalScroll = await studentPage.evaluate(() => {
          return document.documentElement.scrollWidth > document.documentElement.clientWidth;
        });
        const tables = await studentPage.$$eval("table", t => t.length);
        const cards = await studentPage.$$eval("[class*='card']", c => c.length);
        const hasBatalButton = await studentPage.$("button.text-destructive") !== null;

        console.log(`    Screenshot: ${filename}`);
        console.log(`    Tables: ${tables}, Cards: ${cards}`);
        console.log(`    Horizontal scroll: ${hasHorizontalScroll ? "YES (BAD)" : "No (GOOD)"}`);
        console.log(`    Batalkan button: ${hasBatalButton ? "Present (GOOD)" : "Not found"}`);

      } catch (e: any) {
        console.log(`    ERROR: ${e.message}`);
      }
    }
    await studentContext.close();

    // Test sebagai lecturer
    const lecturerContext = await browser.newContext({
      viewport: { width: vp.width, height: vp.height },
    });
    const lecturerPage = await lecturerContext.newPage();
    await loginAsLecturer(lecturerPage);

    // Ketersediaan hanya untuk lecturer
    const pg = PAGES.find(p => p.path === "/ketersediaan")!;
    console.log(`  [lecturer] ${pg.name}...`);
    try {
      await lecturerPage.goto(`${BASE_URL}${pg.path}`, { waitUntil: "networkidle", timeout: 30000 });
      await lecturerPage.waitForTimeout(1000);

      const filename = `${OUTPUT_DIR}/${pg.name}-${vp.name}-lecturer.png`;
      await lecturerPage.screenshot({ path: filename, fullPage: false });

      const hasHorizontalScroll = await lecturerPage.evaluate(() => {
        return document.documentElement.scrollWidth > document.documentElement.clientWidth;
      });
      const forms = await lecturerPage.$$eval("form", f => f.length);
      const inputs = await lecturerPage.$$eval("input", i => i.length);

      console.log(`    Screenshot: ${filename}`);
      console.log(`    Forms: ${forms}, Inputs: ${inputs}`);
      console.log(`    Horizontal scroll: ${hasHorizontalScroll ? "YES (BAD)" : "No (GOOD)"}`);

    } catch (e: any) {
      console.log(`    ERROR: ${e.message}`);
    }
    await lecturerContext.close();
  }

  await browser.close();
  console.log(`\nScreenshots saved to ${OUTPUT_DIR}`);
  console.log("Files:", (await import("fs")).readdirSync(OUTPUT_DIR).join(", "));
}

run().catch(console.error);
