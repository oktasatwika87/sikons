DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS bookings;
DROP TABLE IF EXISTS slots;
DROP TABLE IF EXISTS availability_exceptions;
DROP TABLE IF EXISTS availability_rules;
DROP TABLE IF EXISTS lecturer_profiles;
DROP TABLE IF EXISTS users;

DROP FUNCTION IF EXISTS set_updated_at();

DROP TYPE IF EXISTS notification_status;
DROP TYPE IF EXISTS booking_status;
DROP TYPE IF EXISTS slot_status;
DROP TYPE IF EXISTS user_role;
DROP TYPE IF EXISTS timerange;
