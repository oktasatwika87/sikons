import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // e2e adalah Playwright test: tidak perlu dilint di pipeline CI.
    "e2e/**",
  ]),
  {
    // Larang panggilan Intl.DateTimeFormat.formatRange dan
    // formatRangeToParts, serta semua formatter tanggal/waktu yang
    // membangun teks dari Date di luar web/lib/date.ts. Untuk id-ID,
    // Intl.DateTimeFormat mengeluarkan en-dash (U+2013) di runtime,
    // jadi lolos dari pemindai repo-lint yang membaca isi statis
    // file. Semua rentang tanggal/jam di SIKONS harus lewat helper
    // RANGE_SEPARATOR di lib/date.
    //
    // Pakai no-restricted-syntax dengan selector AST karena instance
    // method call `new Intl.DateTimeFormat().formatRange()` membuat
    // no-restricted-properties tidak bisa mencocokkan objek statis.
    files: ["**/*.{ts,tsx,js,jsx}"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector: "CallExpression[callee.property.name='formatRange']",
          message:
            "Jangan pakai Intl.DateTimeFormat.prototype.formatRange. Pakai formatDateRange/formatTimeRange dari lib/date yang memakai RANGE_SEPARATOR ASCII.",
        },
        {
          selector: "CallExpression[callee.property.name='formatRangeToParts']",
          message:
            "Jangan pakai Intl.DateTimeFormat.prototype.formatRangeToParts. Pakai formatDateRange/formatTimeRange dari lib/date.",
        },
        {
          selector:
            "NewExpression[callee.type='MemberExpression'][callee.property.name='DateTimeFormat']",
          message:
            "Jangan pakai 'new Intl.DateTimeFormat' di luar web/lib/date.ts. Pakai helper dari lib/date (formatJakartaDate, formatJakartaLongDate, formatJakartaShortDate, dll.).",
        },
        {
          selector:
            "CallExpression[callee.property.name='toLocaleDateString']",
          message:
            "Jangan pakai Date.prototype.toLocaleDateString di luar web/lib/date.ts. Pakai helper dari lib/date (formatJakartaDate, formatJakartaLongDate, formatJakartaShortDate).",
        },
        {
          selector:
            "CallExpression[callee.property.name='toLocaleTimeString']",
          message:
            "Jangan pakai Date.prototype.toLocaleTimeString di luar web/lib/date.ts. Pakai helper dari lib/date (formatJakartaTime, formatTimeRange).",
        },
      ],
    },
  },
  // File test boleh memakai Intl/DateTimeFormat/toLocaleDateString
  // untuk setup data (mis. fixture) karena hasilnya tidak tampil ke
  // pengguna. Aturan di blok ini TIDAK berlaku untuk file di bawah.
  {
    files: ["**/__tests__/**", "**/*.test.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
]);

export default eslintConfig;
