-- Rollback migrasi 000003
DROP INDEX IF EXISTS slots_open_lecturer_idx;
ALTER TABLE slots DROP COLUMN IF EXISTS withdrawn_at;
