DROP INDEX IF EXISTS refresh_tokens_family_active_idx;

ALTER TABLE refresh_tokens DROP COLUMN family_id;
