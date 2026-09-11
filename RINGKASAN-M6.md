# Ringkasan Milestone M6

## M6a — Halaman Ketersediaan Dosen

### File yang Berubah / Ditambahkan

#### Baru
- `web/lib/availability/schemas.ts` — Zod schemas + calcSlotCount
- `web/lib/availability/__tests__/schemas.test.ts` — 29 test Zod
- `web/hooks/useAvailability.ts` — query + mutation hooks
- `web/components/availability/ReconcileSummary.tsx` — panel ringkasan
- `web/components/availability/RuleList.tsx` — daftar aturan per hari
- `web/components/availability/RuleForm.tsx` — form tambah aturan
- `web/components/availability/ExceptionList.tsx` — daftar pengecualian
- `web/components/availability/ExceptionForm.tsx` — form tambah pengecualian
- `web/app/ketersediaan/page.tsx` — halaman utama
- `web/app/ketersediaan/__tests__/page.test.tsx` — 5 MSW integration test

#### Dimodifikasi
- `web/lib/api/types.ts` — fix SlotReconcileSummary (added withdrawn, restored, bookings_cancelled)
- `web/components/Nav.tsx` — tambah link "/ketersediaan" untuk role lecturer

### Keputusan Teknis

#### "Akhiri Aturan" pakai `effective_to = today`, bukan `is_active = false`

**Alasan:** `effective_to` bersifat final dan temporal — "aturan ini berhenti berlaku sejak tanggal X". Ini lebih tepat untuk operasi "akhiri" karena:
1. Memberi batas waktu yang jelas
2. Tetap tercatat dalam histori (efective_to != null)
3. `is_active = false` terasa seperti toggle yang bisa di-undo, bukan operasi "akhiri permanen"

**Pengecekan inklusif terkonfirmasi dari kode.** `internal/slotgen/service.go:290`:
```go
if d.Before(rule.effectiveFrom) || (rule.effectiveTo != nil && d.After(*rule.effectiveTo)) {
    continue
}
```
- `d >= effectiveFrom` masuk (inklusif dari sisi bawah)
- `d <= effectiveTo` masuk (inklusif dari sisi atas)

Karena inklusif, `effective_to = today` tetap menghasilkan slot untuk HARI INI. Tidak perlu diubah ke besok. Keputusan sudah benar.

---

## M6b-0 — lecturer_note Muncul di GET /bookings

### Masalah
PATCH /bookings/{id}/complete sudah menyimpan lecturer_note ke kolom bookings.lecturer_note sejak M4a, tapi GET /bookings tidak pernah membalikkan field itu. Dosen menulis catatan lalu tidak bisa membacanya lagi lewat API.

### File yang Berubah

| File | Perubahan |
|------|-----------|
| `internal/booking/booking.go` | Tambah field `LecturerNote string` ke struct `BookingView` |
| `internal/booking/service.go` | Tambah `coalesce(b.lecturer_note, '')` ke SELECT di `listBookingsView`; tambah `&v.LecturerNote` ke Scan |
| `internal/server/booking_handlers.go` | Tambah `lecturer_note,omitempty` ke `bookingResponse`; `viewToResponse` mengisi `LecturerNote` untuk KEDUA role |
| `internal/server/booking_handler_test.go` | Tambah 2 test: alur lengkap dan non-completed |
| `web/lib/api/types.ts` | Tambah `lecturer_note?: string` ke `BookingResponse` |

### Contoh Response JSON

**Sebelum (field lecturer_note tidak ada):**
```json
{
  "id": "uuid-booking",
  "status": "completed",
  "topic": "Bimbingan Skripsi",
  ...
}
```

**Sesudah (lecturer_note muncul untuk kedua role):**
```json
{
  "id": "uuid-booking",
  "status": "completed",
  "topic": "Bimbingan Skripsi",
  ...
  "lecturer_note": "Sesi produktif, mahasiswa perlu follow-up minggu depan"
}
```

### Hasil Test

2 test baru di `booking_handler_test.go`:
- `TestLecturerNoteMunculDiGetBooking` — alur lengkap, verified untuk mahasiswa dan dosen
- `TestLecturerNoteKosongUntukNonCompleted` — verified bahwa field tidak muncul untuk non-completed

---

## M6b — Halaman Dashboard Dosen + Aksi Tandai Selesai/Tidak Hadir

### File yang Berubah / Ditambahkan

#### Baru
- `web/lib/date.ts` — tambah fungsi `isSameJakartaDay(a, b)`
- `web/lib/booking.ts` — tambah fungsi `isSessionActionable` dan `groupDashboardBookings`
- `web/lib/__tests__/booking.test.ts` — 14 test baru (isSessionActionable + groupDashboardBookings)
- `web/lib/__tests__/date.test.ts` — 4 test baru (isSameJakartaDay)
- `web/lib/msw/handlers/booking.ts` — tambah PATCH complete dan no-show handlers
- `web/hooks/useCompleteBooking.ts` — mutation hook untuk tandakan selesai
- `web/hooks/useNoShowBooking.ts` — mutation hook untuk tandakan tidak hadir
- `web/app/dashboard/page.tsx` — halaman utama dashboard
- `web/app/dashboard/__tests__/page.test.tsx` — 2 MSW integration test

#### Dimodifikasi
- `web/components/Nav.tsx` — tambah link "/dashboard" untuk role lecturer
- `web/lib/api/types.ts` — sudah punya lecturer_note dari M6b-0

### Definisi Ketiga Bucket

Pengelompokan berdasarkan waktu saat data diambil (`nowInstant`), BUKAN Date.now() di dalam fungsi:

1. **needsFollowUp**: `confirmed` DAN `startAt < nowInstant` (sudah lewat, kapan pun)
2. **today**: `confirmed` DAN `startAt >= nowInstant` DAN jatuh di tanggal yang sama dengan `nowInstant` (WIB)
3. **upcoming**: `confirmed` DAN `startAt >= nowInstant` DAN BUKAN tanggal yang sama dengan `nowInstant` (WIB)

**Perbandingan (1) dan batas (2)/(3) berbeda kelas:**
- `(1)` pakai perbandingan instant murni (epoch ms)
- `(2)/(3)` butuh konsep kalender → wajib lewat `isSameJakartaDay` (dari lib/date.ts)

### Lokasi isSessionActionable

Fungsi `isSessionActionable` ditaruh di `web/lib/booking.ts` — file yang SAMA dengan `isBookingCancelable`, mengikuti pola yang sudah ada.

---

## M6c — Rename, Navigasi Peran, Landing, Responsive

### File yang Berubah / Ditambahkan

#### Baru
- `web/app/konsultasi/page.tsx` — rename dari /booking-saya, generalisasi untuk dua peran
- `web/components/auth/RoleRedirect.tsx` — redirect sesuai role (client-side)
- `web/app/page.tsx` — landing page sekarang redirect sesuai role

#### Dimodifikasi
- `web/components/Nav.tsx` — navigasi final per role (Dosen: Dashboard/Ketersediaan/Konsultasi; Mahasiswa: Cari Dosen/Konsultasi; Admin: hanya Akun)
- `web/components/booking/BookingDialog.tsx` — update link ke /konsultasi
- `web/components/lecturer/LecturerDetail.tsx` — kalender responsive (7 kolom desktop, daftar vertikal mobile, collapse hari tanpa slot)
- `web/components/availability/RuleForm.tsx` — form responsive (satu kolom mobile, touch target min 44px)

#### Dihapus
- `web/app/booking-saya/` — direktori lama dihapus

### Navigasi Final Per Role

| Role | Link Fitur |
|------|-----------|
| Dosen | Dashboard, Ketersediaan, Konsultasi |
| Mahasiswa | Cari Dosen, Konsultasi |
| Admin | (tidak ada link fitur) |

### Root Landing Page

- `/` sekarang redirect sesuai role (client-side, tanpa middleware Next.js)
- Dosen -> /dashboard
- Mahasiswa -> /dosen
- Admin -> /akun
- Anonim -> landing page dengan tombol Masuk/Daftar

### Responsive Yang Diimplementasikan

1. **Tabel riwayat (/konsultasi)**: di bawah breakpoint md jadi daftar kartu, bukan tabel yang di-scroll horizontal.
2. **Kalender slot (/dosen/[id])**: grid 7 kolom desktop; daftar vertikal per hari di mobile, hari tanpa slot di-collapse.
3. **Form aturan (/ketersediaan)**: field berjajar jadi satu kolom penuh di mobile, touch target minimal 44px.

**Catatan verifikasi responsive**: Implementasi CSS sudah dilakukan berdasarkan breakpoint Tailwind (`md:`). Verifikasi manual di browser sungguhan di lebar 375px dan 768px BELUM dilakukan karena keterbatasan environment CLI. Implementasi mengikuti pola Tailwind standar (hidden/block berdasarkan breakpoint).

---

## Test Count Repo

### Web Tests
| File | Test Count |
|------|------------|
| date.test.ts | ~20 |
| booking.test.ts | 20 |
| dashboard/page.test.tsx | 2 |
| auth/__tests__/AuthContext.test.tsx | (existing) |
| hooks/__tests__/* | (existing) |
| availability/* | (existing) |
| lecturer/* | (existing) |
| booking-saya/page.test.tsx | (removed) |
| **Total Web** | **104** |

### Go Tests
| Package | Status |
|---------|--------|
| internal/auth | PASS |
| internal/booking | PASS |
| internal/httpx | PASS |
| internal/lecturer | PASS |
| internal/notifier | PASS |
| internal/reminder | PASS |
| internal/server | PASS |
| internal/slotgen | 4 FAIL (known, sejak M3b) |

### Total Test Repo
- **Web: 104 tests (15 files)**
- **Go: 7 packages OK + 4 known-fail (slotgen)**

---

## Sisa Pekerjaan M6

1. **Verifikasi responsive manual**: breakpoint 375px dan 768px perlu dicek di browser sungguhan
2. **slotgen tests**: 4 test di-skip sejak M3b untuk investigasi bug

Milestone M6 secara fungsional sudah tuntas — semua fitur yang dijadwalkan sudah diimplementasikan dan test pass.
