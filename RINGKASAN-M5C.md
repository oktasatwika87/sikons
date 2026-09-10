# RINGKASAN — M5c: Cari Dosen

## Catatan soal file rencana

File kontrak `sikons-m5-rencana-frontend.md` yang dirujuk prompt tidak ada di
repo (`git log --all --oneline -- '*rencana*'` kosong, grep tidak menemukan).
Yang ada hanya RINGKASAN-M5B.md (working note lokal, untracked). Aku jalankan
M5c mengikuti ringkasan 8 keputusan yang sudah ditulis ulang di prompt dan
file-file referensi yang ada (types.ts, service.go, cmd/seed/main.go).

Pertanyaan terbuka tentang kontrak: apakah file rencana asli masih dihapus
sepanjang jalan, atau perlu dibuat ulang untuk M5d? Aku tidak mengarang untuk
mengisinya — lihat bagian "Pertanyaan terbuka".

## Yang dibangun

### Dependensi baru (`web/package.json`)

- `@tanstack/react-query@^5.102.8` — pertama kali dipasang di proyek.
- `date-fns@^4.4.0` + `date-fns-tz@^3.2.0` — konsisten dengan singkat
  `date-fns` di brief (sudah disebut di M5a). Dipakai lib/date.ts.

`npm install` melewati peer warning `vitest@5` vs `@types/node@20`; pakai
`--legacy-peer-deps` karena yang lain di proyek ini sudah bertoleransi.

### Root layout (`web/app/layout.tsx`, `web/components/Providers.tsx`)

`AuthLayout` dari M5b dihapus; fungsinya pindah ke `Providers` baru yang
membungkus:

  QueryClientProvider → AuthProvider → Nav + children

`QueryClient` dibangun lewat `useState(() => new QueryClient(...))` — bukan
module scope, supaya tiap request server dapat instance terpisah (gotcha
klasik Next.js App Router). Retry 1× untuk error jaringan; refresh-on-focus
nonaktif (default M5c untuk halaman browsing publik).

`Providers` dipilih namanya generik, bukan "AuthLayout", karena sudah
melampaui auth — keputusan M5c #1.

### Utilitas tanggal (`web/lib/date.ts`)

Semua tampilan waktu dari backend lewat sini. Ekspor fungsi:

- `formatJakartaTime(value)` — "HH:mm"
- `formatJakartaShortDate(value)` — "10 Sep"
- `formatJakartaLongDate(value)` — "Jumat, 11 September 2026"
- `jakartaDateKey(value)` — "YYYY-MM-DD"
- `jakartaDayName(value)` — "Senin" .. "Minggu"
- `startOfWeekJakarta(value?)` — Date UTC untuk Senin 00:00 WIB
- `weekRangeJakarta(value?)` — `{ from, to }` Senin..Senai+7
- `weekDayHeaders(value?)` — 7 header Senin..Minggu
- `weekDaysJakarta(value?)` — 7 Date UTC Senin..Minggu
- `formatWeekRangeLabel(value?)` — "7–13 September 2026"
- `isBeforeThisWeekJakarta(value)` — untuk disable tombol "Sebelumnya"
- `groupSlotsByJakartaDate<T>(slots)` — Map<dateKey, T[]>

Konstanta `TIMEZONE = "Asia/Jakarta"` diekspor — satu sumber kebenaran.

**Catatan penting dari debugging awal** (lihat RINGKASAN-M5B catatan soal
bug closure di useApiFetch; pelajaran yang sama):

- `format(date, ...)` dari date-fns SELALU pakai zona lokal runtime —
  di jsdom TZ=UTC atau macOS dev TZ=Asia/Makassar hasilnya beda. SOLUSI:
  pakai `formatInTimeZone(date, "Asia/Jakarta", fmt)` untuk setiap
  ekstraksi string. `toZonedTime` saja tidak cukup.
- ISO weekday di date-fns v4 = token `i` (1=Senin..7=Minggu). Di v3 dulu
  `i` adalah local weekday (1=Minggu). Setelah naik ke v4, format InTimeZone
  dengan `i` langsung ISO — tidak perlu konversi tambahan.

Implementasi detail keputusan ini ada di header komentar `lib/date.ts`.

### Hook debounce (`web/hooks/useDebouncedValue.ts`)

Hook kecil sendiri, tanpa `lodash.debounce` atau `use-debounce` (keputusan
M5c #3). Pola `useState + setTimeout` dengan cleanup di effect.

### Halaman `/dosen` (`web/app/dosen/page.tsx`, `web/components/lecturer/LecturerList.tsx`)

- Filter `department`, `q`, `page` disimpan di URL query string lewat
  `useSearchParams` + `router.replace`. Back/forward mengembalikan filter
  yang sama.
- `q` di-debounce ~350ms sebelum ditulis ke URL — lihat `useDebouncedValue`.
- `LecturerList` pakai query key `['lecturers', { department, q, page }]`
  (keputusan #2). `placeholderData: keepPreviousData` supaya pindah halaman
  tidak flicker.
- Pattern sync state lokal dari URL: pakai "store & compare" dengan dua
  `useState` (React docs resmi untuk kasus ini) — bukan `useEffect`
  yang melakukan `setState` sinkron (di React 19 ini anti-pattern, di-flag
  `react-hooks/set-state-in-effect`).
- Dropdown `department` nilainya hardcoded dari `web/components/lecturer/
  departments.ts`. Nilainya dicek silang ke `cmd/seed/main.go` baris 90–91
  persis: `"Informatika"`, `"Sistem Informasi"`. Komentar di file itu
  menjelaskan kalau seed berubah, ubah sini (keputusan #5). Kalau nanti
  ada endpoint backend untuk daftar department, ganti jadi fetch.
- Pagination sederhana: tombol Sebelumnya/Berikutnya, nonaktif di batas.

### Halaman `/dosen/[id]` (`web/app/dosen/[id]/page.tsx`, `web/components/lecturer/LecturerDetail.tsx`)

- Query `['lecturer', id]` untuk detail, `['lecturer', id, 'slots', {from,
  to}]` untuk jadwal.
- Kalender minggu: 7 kolom Senin–Minggu, masing-masing list chip waktu
  slot. Header "Senin", sub-header tanggal singkat via
  `formatJakartaShortDate`.
- Slot `status === "booked"` → chip disabled dengan line-through. Slot
  `open` tetap bisa diklik tapi TIDAK memicu apa pun di M5c (lihat pesan
  tooltip + footer di bawah kalender). Booking form adalah M5d — keputusan
  #6 eksplisit.
- Navigasi prev/next: anchor disimpan di `useState`, BUKAN URL (kalender
  bukan filter list; tidak perlu share-link aware). Tombol "Sebelumnya"
  nonaktif kalau `isBeforeThisWeekJakarta(weekAnchor)` — keputusan #6.
- Default minggu berjalan: `useState(() => new Date())`. Tidak ada kalender
  eksternal; murni grid CSS.

### Nav (`web/components/Nav.tsx`)

Tambah link "Cari Dosen" di header, konsisten dengan gaya link yang
sudah ada (Tanpa auth, siapa pun boleh browse — endpoint publik).

## Test

Total **38 test lulus** (sebelumnya 21 di M5b, sekarang +17).

| Berkas                                                              | Test | Topik |
|---------------------------------------------------------------------|------|-------|
| `web/lib/__tests__/date.test.ts`                                    | 13   | Konversi zona Asia/Jakarta (termasuk lintas batas hari UTC, ISO weekday Senin..Minggu, startOfWeek Senin, weekRangeJakarta ke-iso-kan, groupSlotsByJakartaDate, weekDayHeaders/weekDaysJakarta, isBeforeThisWeekJakarta, formatWeekRangeLabel, formatJakartaLongDate, formatJakartaShortDate). |
| `web/hooks/__tests__/useDebouncedValue.test.ts`                     | 2    | Delay pembaruan nilai pakai fake timers; pembaruan berturut-turut dalam delay direset ke perubahan TERAKHIR. |
| `web/components/lecturer/__tests__/LecturerList.test.tsx`           | 2    | MSW: filter URL (`department`, `q`, `page`) diteruskan utuh ke `/api/v1/lecturers`; URL tanpa filter → `/api/v1/lecturers` tanpa query string. |

Berkas test lama (M5a/M5b) tetap hijau:

- `web/lib/auth/__tests__/schemas.test.ts`
- `web/lib/auth/__tests__/AuthContext.test.tsx`
- `web/components/auth/__tests__/LoginForm.test.tsx`
- `web/components/auth/__tests__/RequireAuth.test.tsx`
- `web/hooks/__tests__/useApiFetch.test.tsx`

## Bug/regresi yang ditemukan saat implementasi

### 1. `format()` date-fns pakai zona lokal, bukan zona target

Gejala: 5 test gagal awal — `startOfWeekJakarta` untuk Kamis 10 Sep
mengembalikan tanggal 6 Sep (Minggu) alih-alih 7 Sep (Senin).

Penyebab: `format(zonedDate, "yyyy-MM-dd")` dari date-fns SELALU pakai
zona lokal runtime. `toZonedTime` memang memutasikan internal timestamp
Date supaya `getDate()`/`getHours()` di zona target — tapi `format()`
masih pakai zona lokal. Di macOS dev TZ=Asia/Makassar, di jsdom TZ=UTC,
di CI Linux TZ=UTC — hasil beda.

Cara tahu: tulis test sederhana yang membandingkan `getDate()` (yang
mengikuti zona lokal sebenarnya) vs `format()` output. Setelah pakai
`formatInTimeZone(date, "Asia/Jakarta", fmt)` untuk semua ekstraksi
string, semua test ijo.

Pelajaran: ini kelas bug yang persis sama dengan M3b di backend (jam
dev WITA vs kampus WIB) — disebut di keputusan #7 prompt. Tidak boleh
berulang di frontend.

### 2. ISO weekday token beda antara date-fns v3 dan v4

Gejala: 8 test gagal setelah fix #1. Hari yang diharapkan "Senin"
di-output sebagai "Jumat" (offset 6).

Penyebab: aku pakai konversi `local === 1 ? 7 : local - 1` dengan asumsi
token `i` adalah local weekday date-fns v3. Setelah naik ke date-fns v4
(via `npm install date-fns@^4`), `i` adalah ISO weekday (1=Senin..7=
Minggu).

Cara tahu: jalankan `formatInTimeZone(senin2026, "i")` di repl Node,
lihat outputnya langsung 1, bukan 2.

Fix: hapus konversi, pakai nilai `i` langsung sebagai ISO weekday.
Komentar header `lib/date.ts` sekarang menyebut versi date-fns yang
dipakai supaya pembaca berikutnya tidak salah asumsi.

### 3. eslint `react-hooks/set-state-in-effect` untuk sync URL → state

Gejala: `npm run lint` error di `LecturerList.tsx`. Pola `useState(q)`
untuk input lokal + `useEffect` untuk sinkronkan input ketika URL `q`
berubah (back/forward) ditolak oleh React 19.

Fix: pakai pola "store & compare" dari React docs resmi — `useState`
kedua untuk nilai sinkronisasi terakhir, bandingkan di body komponen
dan set state bila berbeda. React menerima `setState` di render asal
memang dari pola ini; eslint tidak komplain.

## Verifikasi

- `npm run lint` — 0 error. 1 warning pre-existing untuk
  `mockServiceWorker.js` (di luar scope).
- `npm test` — 38 test ijo (sebelumnya 21).
- `npm run build` — sukses, TypeScript bersih. Route baru:
  `○ /dosen` (static) dan `ƒ /dosen/[id]` (dynamic karena
  `params: Promise<{...}>` di Next 16).

## Berkas yang berubah/ditambah

| File | Perubahan |
|------|-----------|
| `web/package.json` | Tambah `@tanstack/react-query`, `date-fns`, `date-fns-tz`. |
| `web/package-lock.json` | Lock file hasil `npm install`. |
| `web/app/layout.tsx` | Pakai `Providers` alih-alih `AuthLayout`. |
| `web/components/Providers.tsx` | **Baru** — QueryClientProvider + AuthProvider + Nav. |
| `web/components/auth/AuthLayout.tsx` | **Dihapus** — fungsinya pindah ke `Providers`. |
| `web/components/Nav.tsx` | Tambah link "Cari Dosen". |
| `web/lib/date.ts` | **Baru** — single source of truth zona Asia/Jakarta. |
| `web/lib/__tests__/date.test.ts` | **Baru** — 13 test. |
| `web/hooks/useDebouncedValue.ts` | **Baru** — hook debounce. |
| `web/hooks/__tests__/useDebouncedValue.test.ts` | **Baru** — 2 test pakai fake timers. |
| `web/components/lecturer/departments.ts` | **Baru** — konstanta department dicek silang ke `cmd/seed`. |
| `web/components/lecturer/LecturerList.tsx` | **Baru** — list + filter + pagination. |
| `web/components/lecturer/LecturerDetail.tsx` | **Baru** — detail + kalender slot. |
| `web/components/lecturer/__tests__/LecturerList.test.tsx` | **Baru** — 2 test MSW. |
| `web/app/dosen/page.tsx` | **Baru** — server shell + Suspense. |
| `web/app/dosen/[id]/page.tsx` | **Baru** — server shell + Suspense, baca `params` async. |

## Keputusan lain (di luar scope M5c murni)

### Perbaikan bug `/akun` ikut satu paket M5c, bukan M5b

`web/app/akun/page.tsx` (komit M5b) memakai
`new Date(user.created_at).toLocaleDateString("id-ID", ...)` untuk
menampilkan tanggal bergabung — kelas bug zona waktu yang persis sama
dengan yang dilarang di keputusan #7. Bukan waktu slot, tapi
`created_at` adalah timestamptz UTC juga; pakai `toLocaleDateString`
artinya tampilan ikut zona browser, bukan Asia/Jakarta.

Keputusan: ganti dengan `formatJakartaLongDate(user.created_at)` dan
disatukan ke komit M5c (bukan amend ke M5b). Alasannya:
- `formatJakartaLongDate` berada di `lib/date.ts` yang baru ada di M5c.
  Backport ke M5b berarti komit M5b membawa file M5c, yang kontra
  produktif dengan pemisahan historis yang sudah dipilih.
- Perubahan kecil (satu baris), eksplisit disebut di sini supaya
  pembaca diff tidak bingung kenapa `app/akun/page.tsx` muncul di komit
  M5c yang temanya "cari dosen".
- Setara dengan kelas bug yang sedang ditangani M5c — kalau tidak
  sekarang, kemungkinan lupa.

Tidak ada efek samping: dependensi import `formatJakartaLongDate`
sudah ditambahkan ke `app/akun/page.tsx`. Test /akun tidak ada (di M5b
hanya dites sebagai "RequireAuth redirect setelah anonymous" lewat
RequireAuth.test.tsx, bukan render penuh halaman), jadi tidak ada test
yang harus di-update.

## Pertanyaan terbuka

### 1. Kontrak M5c tidak punya file di repo

`sikons-m5-rencana-frontend.md` yang dirujuk prompt tidak ada; hal yang
sama untuk `sikons-keputusan-desain.md` dan `brief` (kalau itu berarti
brief tertulis). Aku jalankan M5c mengikuti ringkasan 8 keputusan di
prompt. Apakah file rencana asli perlu dibuat ulang (untuk M5d dan
seterusnya), atau cara dokumentasi lain yang kamu lebih suka?

### 2. Apakah Nav "Cari Dosen" sebaiknya disembunyikan saat login sebagai dosen?

Saat dosen login, "Cari Dosen" masih muncul — bisa diklik karena
endpoint publik. Untuk konsistensi role-aware UI, mungkin disembunyikan
saja untuk role lecturer. Tapi di M3 tidak ada halaman dashboard dosen
— dosen belum punya "home page" selain `/akun`. Mungkin lebih baik
tunda sampai ada landing khusus dosen (M5e/M6).

Tidak urgent; tidak dilakukan di M5c.
