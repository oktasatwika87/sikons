# RINGKASAN M5d: Flow Booking + Halaman Booking Saya

## Apa yang Dibangun

### Komponen UI Baru
- `web/components/ui/dialog.tsx` — Dialog component manual (bukan @base-ui — simpler)
- `web/components/ui/alert-dialog.tsx` — AlertDialog untuk konfirmasi cancel

### Custom Hooks
- `web/hooks/useCreateBooking.ts` — Hook untuk membuat booking dengan:
  - Idempotency key (crypto.randomUUID) dibuat sekali saat hook mount
  - Query invalidation otomatis untuk kalender dan list booking
- `web/hooks/useCancelBooking.ts` — Hook untuk membatalkan booking

### Komponen Booking
- `web/components/booking/BookingDialog.tsx` — Dialog form booking dengan:
  - Topic (required, 3-200 char) + description (optional, max 2000)
  - Error handling berbeda untuk setiap error code:
    - `SLOT_ALREADY_BOOKED` (409): callback ke parent untuk banner
    - `SLOT_TOO_SOON` (422): tampilkan pesan di dalam dialog, dialog tetap terbuka
    - `BOOKING_LIMIT_REACHED` (409): tampilkan pesan + link ke /booking-saya

### Halaman
- `web/app/booking-saya/page.tsx` — Halaman list booking dengan:
  - RequireAuth wrapper
  - Filter status tabs (Semua / Akan Datang / Selesai / Dibatalkan / Tidak Hadir)
  - Pagination dengan page di URL query param
  - Tombol Cancel untuk student dengan booking confirmed
  - Disable client-side untuk booking < H-3 jam

### Helper Functions
- `web/lib/booking.ts` — Helper functions:
  - `isBookingCancelable(slotStartAt)` — Pure Date.now() comparison untuk cek batas 3 jam
  - `bookingStatusLabel(status)` — Label status dalam Bahasa Indonesia
  - `bookingStatusClass(status)` — CSS class untuk badge status

### Update Existing
- `web/components/lecturer/LecturerDetail.tsx` — Klik slot open membuka BookingDialog + banner error
- `web/components/Nav.tsx` — Link "Booking Saya" untuk user authenticated

### Test Files
- `web/hooks/__tests__/useCreateBooking.test.tsx` — 7 test
- `web/lib/__tests__/booking.test.ts` — 6 test untuk isBookingCancelable
- `web/app/booking-saya/__tests__/page.test.tsx` — 1 test untuk alur cancel
- `web/components/lecturer/__tests__/LecturerDetail.test.tsx` — 1 test untuk banner error

## Keputusan Teknis

### Error Codes (dikonfirmasi dari `internal/server/booking_handlers.go`)
| Code | HTTP Status | Aksi UI |
|------|-------------|---------|
| `SLOT_ALREADY_BOOKED` | 409 | Banner muncul di halaman, invalidate kalender |
| `BOOKING_LIMIT_REACHED` | 409 | Pesan di dalam dialog + link ke /booking-saya |
| `SLOT_TOO_SOON` | 422 | Pesan di dalam dialog, dialog tetap terbuka |
| `CANCEL_TOO_LATE` | 422 | Disable tombol cancel (sudah di-H-3 jam) |

### Endpoint Cancel
- **PATCH** `/api/v1/bookings/{id}/cancel` (PATCH, BUKAN POST — konfirmasi dari `booking_handler_test.go:893`)

### Idempotency Key
- Header `Idempotency-Key` dengan lazy `crypto.randomUUID()` saat dialog mount
- Key sama untuk SEMUA retry selama dialog terbuka
- Frontend TIDAK menghitung atau mengirim request_hash — server menghitung sendiri dari body yang diterima (slot_id + topic + description). Ini menghilangkan risiko hash mismatch antara frontend dan backend.

### Idempotency Request Hash

**Server-side computation only.** Backend menghitung hash dari body yang diterimanya:

```go
// internal/booking/service.go
func ComputeRequestHash(slotID, topic, description string) string {
    data := slotID + "\n" + topic + "\n" + description
    sum := sha256.Sum256([]byte(data))
    return hex.EncodeToString(sum[:])
}
```

Frontend hanya mengirim `Idempotency-Key` header. Tidak ada hash computation di frontend.

Kalau hash mismatch untuk key yang sama:
- Backend balas: **422 `IDEMPOTENCY_KEY_REUSED`** (`internal/server/booking_handlers.go:147-149`)
- Kondisi ini tidak terjadi dengan implementasi saat ini karena frontend tidak mengirim hash

### isBookingCancelable
- Pure `Date.now()` comparison, BUKAN `lib/date.ts`
- `slotStartAt.getTime() - Date.now() > 3 * 60 * 60 * 1000`
- Tidak butuh mock timezone

### Filter Status Tabs di /booking-saya

**Server-side**, bukan client-side.

Filter dikirim sebagai query param `?status=confirmed` ke `GET /api/v1/bookings`. Backend (service) menghitung ulang total dari database. Page dan filter adalah parameter URL, jadi bookmarkable dan shareable.

Ini berarti: tab filter menunjukkan hasil yang KONSISTEN dengan pagination — tidak ada risiko item tersembunyi di halaman pagination lain.

**Catatan:** Parameter `?status=` sudah ada di backend sejak commit `c9e3eeb` (lapisan HTTP booking). Bukan perubahan M5d.

### Semua Endpoint Pakai Prefix `/api/v1`

Konfirmasi: Ya, semua endpoint backend konsisten pakai `/api/v1`:
- `POST /api/v1/bookings`
- `GET /api/v1/bookings`
- `PATCH /api/v1/bookings/{id}/cancel`
- `GET /api/v1/auth/me`
- dll.

## Hasil Test

```
Test Files  12 passed (12)
     Tests  53 passed (53)
```

Test penting yang lulus:
- Idempotency key SAMA di request pertama dan retry (tanpa menutup dialog)
- isBookingCancelable batas 3 jam (tepat di bawah, tepat di atas, jauh di atas)
- Alur cancel (klik → konfirmasi dialog → dialog terbuka)
- Banner error muncul setelah SLOT_ALREADY_BOOKED

## Catatan Teknis

1. **useCreateBooking error mapping** — TanStack Query `useMutation` `onError` callback menerima typed error. Component harus check `err.type` untuk mencabangkan aksi UI. Kode yang tidak dikenali jatuh ke fallback `unknown` → "Terjadi kesalahan."

2. **Dialog implementation** — Manual/simple implementation, bukan dari @base-ui/react karena kompleksitas TypeScript types. Backdrop + panel fixed position sudah cukup untuk use case ini.

3. **MSW handlers** — `web/lib/msw/` untuk intercept API calls di test.

## Pertanyaan Terbuka

1. **Toast notification** — Belum ada komponen toast di UI. Pesan error untuk SLOT_ALREADY_BOOKED sekarang menggunakan banner dismissable. Apakah perlu ditambahkan toast untuk sukses booking?

2. **Polling invalidation** — Setelah cancel berhasil, list booking di-invalidasi. Apakah perlu ada polling untuk refresh status secara periodik?
