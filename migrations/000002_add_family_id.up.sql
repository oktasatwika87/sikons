-- =============================================================================
-- M2b: refresh token rotation dengan family_id untuk deteksi pencurian token
-- =============================================================================

-- Tambah kolom family_id. Default gen_random_uuid() mengisi baris lama yang belum
-- punya — token tersebut tidak bisa dirotasi karena tidak ada family, tapi
-- baris-baris itu memang sudah tidak dipakai (sistem refresh token belum ada).
ALTER TABLE refresh_tokens
    ADD COLUMN family_id uuid NOT NULL DEFAULT gen_random_uuid();

-- Hapus default — token baru HARUS diisi saat Issue, bukan dari default kolom.
ALTER TABLE refresh_tokens
    ALTER COLUMN family_id DROP DEFAULT;

-- Index untuk cepat mencabut seluruh family saat reuse terdeteksi, atau saat
-- logout. revoked_at IS NULL memastikan hanya token aktif yang terpengaruh.
CREATE INDEX refresh_tokens_family_active_idx
    ON refresh_tokens (family_id) WHERE revoked_at IS NULL;
