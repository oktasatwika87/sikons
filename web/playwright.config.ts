import { defineConfig, devices } from "@playwright/test";

/**
 * Konfigurasi Playwright untuk test E2E SIKONS.
 *
 * Asumsi lingkungan:
 * - Postgres sudah berjalan lewat `docker compose up -d`.
 * - target `make e2e` sudah menjalankan migrate, seed, generate-slots,
 *   `go build` ke `./bin/api`, dan `next build` (jadi `next start` bisa jalan).
 * - Server API jalan di :8080 (default cmd/api) dan Next.js di :3000.
 *
 * Pola:
 * - Hanya chromium. workers 1 + fullyParallel false supaya tidak ada balapan
 *   antar-test terhadap DB.
 * - testMatch *.spec.ts -> file `e2e/responsive-screenshots.ts` TIDAK ikut
 *   jalan. Skrip itu manual (`tsx e2e/responsive-screenshots.ts`).
 * - timezoneId Asia/Jakarta, sesuai zona tampilan aplikasi.
 * - baseURL http://localhost:3000 karena Next.js default.
 * - webServer ARRAY: api (binary) + next start. BaseURL harus eksplisit kalau
 *   webServer array (aturan Playwright).
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.spec.ts",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { outputFolder: "./playwright-report", open: "never" }]],
  outputDir: "./test-results",

  use: {
    baseURL: "http://localhost:3000",
    trace: "retain-on-failure",
    timezoneId: "Asia/Jakarta",
  },

  // projects: hanya chromium, seperti instruksi M7b-1.
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],

  webServer: [
    {
      // API: binary hasil `go build -o bin/api ./cmd/api`. BUKAN `go run`;
      // `go run` menjaga source tree terkunci dan menambah startup time.
      // cwd: '..' = root project (sikons), supaya .env + bin/ di-load benar.
      // Env (DATABASE_URL, JWT_SECRET, CORS_ALLOWED_ORIGINS, dst) diwarisi
      // dari parent process (`make e2e` sudah export .env).
      command: "./bin/api",
      cwd: "..",
      url: "http://localhost:8080/healthz",
      timeout: 60_000,
      reuseExistingServer: !process.env.CI,
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      // Next.js: `next start`, BUKAN `next dev`: supaya tidak ada
      // hot-reload yang bisa ganggu test. next start butuh output `next build`
      // yang dibuat sebelum `make e2e` menjalankan Playwright.
      // Perhatikan: -p 3000 dieksplisitkan supaya sesuai baseURL dan CORS.
      command: "npx next start -p 3000",
      cwd: ".",
      url: "http://localhost:3000",
      timeout: 60_000,
      reuseExistingServer: !process.env.CI,
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});
