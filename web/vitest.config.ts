import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "path";

// Zona waktu proses test dipatok ke UTC, sama dengan runner CI. Laptop di
// WITA cuma beda 1 jam dari WIB, jadi test yang tidak sengaja bergantung zona
// mesin gampang lolos di lokal tapi gagal di CI. Dengan UTC (beda 7 jam),
// masalah seperti itu kelihatan di dua tempat sekaligus.
process.env.TZ = "UTC";

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
});
