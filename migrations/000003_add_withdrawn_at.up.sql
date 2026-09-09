-- =============================================================================
-- M3b: slot generator dengan pendekatan rekonsiliasi
-- =============================================================================
--
-- LATAR BELAKANG:
-- Seorang dosen mengubah aturan ketersediaan → slot yang sudah ada harus
-- disesuaikan. Beberapa kasus yang harus ditangani:
--
--   1. Slot baru perlu dibuat     → INSERT
--   2. Slot lama tidak relevan   → DELETE (kalau belum pernah dibooking)
--                                 → SET withdrawn_at (kalau sudah ada riwayat)
--   3. Slot withdrawn harus dikembalikan kalau aturan kembali aktif
--
-- Kenapa tidak pakai DELETE + INSERT biasa?
-- Karena foreign key RESTRICT pada bookings(slot_id) menahan penghapusan slot
-- yang sudah punya riwayat. Menghapus slot berarti menghapus histori, dan
-- itu tidak boleh. Solusinya: instead of deleting, mark as withdrawn.
--
-- TAMBAHAN:
-- Index baru (lecturer_id, start_at) WHERE status='open' AND withdrawn_at IS NULL
-- dipakai untuk query listing slot yang bisa dipesan mahasiswa. Sebelum ada
-- withdrawn_at, query ini tidak bisa memfilter slot yang ditarik.

-- ---------------------------------------------------------------------------
-- slots: kolom withdrawn_at
-- ---------------------------------------------------------------------------
ALTER TABLE slots ADD COLUMN withdrawn_at timestamptz;

-- ---------------------------------------------------------------------------
-- index untuk query slot yang bisa dipesan
-- ---------------------------------------------------------------------------
CREATE INDEX slots_open_lecturer_idx
    ON slots (lecturer_id, start_at)
    WHERE status = 'open' AND withdrawn_at IS NULL;
