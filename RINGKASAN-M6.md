# Ringkasan M6a — Halaman Ketersediaan Dosen

## File yang Berubah / Ditambahkan

### Baru
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

### Dimodifikasi
- `web/lib/api/types.ts` — fix SlotReconcileSummary (added withdrawn, restored, bookings_cancelled)
- `web/components/Nav.tsx` — tambah link "/ketersediaan" untuk role lecturer

---

## Keputusan Teknis

### "Akhiri Aturan" pakai `effective_to = today`, bukan `is_active = false`

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

## Hasil Verifikasi Manual Responsive

Belum dilakukan. Butuh dicek di browser dengan lebar 375px (iPhone SE).

Belum dilakukan. Butuh dicek di browser dengan lebar 375px (iPhone SE).

---

## Test Count

| File | Count |
|------|-------|
| schemas.test.ts | 29 |
| page.test.tsx | 5 |
| **Total M6a** | **34** |

Total test repo setelah M6a: **87** (sebelumnya 53)

---

## Catatan Implementasi

1. **Summary panel** muncul sebagai panel menetap (bukan toast) di atas halaman setelah mutasi. Styling destructif (`border-destructive/50`, `bg-destructive/5`, badge merah) aktif hanya kalau `bookings_cancelled > 0`.

2. **Preview slot count** di form aturan dihitung di klien dari form values (`calcSlotCount`), bukan dari API.

3. **Rentang pengecualian**: from = hari ini, to = +90 hari. Dihitung di `getExceptionRange()` di page component.

4. **Query invalidation**: Setelah mutasi, invalidate `availability-rules`, `availability-exceptions`, DAN `lecturer/{id}/slots`. ID lecturer diambil dari `auth.user?.id` (lecturer profile).

5. **Error ATURAN_BENTROK**: Langsung tampil di form dengan pesan spesifik dari backend.

6. **Dialog konfirmasi**: Pakai AlertDialog yang sudah ada, dengan teks yang tidak menjanjikan angka pasti (sesuai spec).

---

## Pertanyaan Terbuka (Belum Ada Jawaban dari Kode)

1. Apakah perlu ada link "Ketersediaan" di Nav untuk admin? Konteks: spec hanya menyebut dosen, tapi admin juga bisa punya role lecturer? Atau apakah role lecturer eksklusif untuk dosen?
