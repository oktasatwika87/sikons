-- =============================================================================
-- SIKONS — skema awal
--
-- Aturan yang dipegang di seluruh file ini:
--   1. Semua waktu absolut disimpan timestamptz (UTC). Waktu "jam dinding" yang
--      berulang tiap minggu (aturan ketersediaan) disimpan sebagai `time` polos,
--      karena dia memang tidak punya tanggal dan tidak punya zona.
--   2. Kalau sebuah keadaan tidak boleh terjadi, dilarang lewat CONSTRAINT,
--      bukan lewat if di Go. Constraint tidak bisa lupa dijalankan.
--   3. Satu fakta hidup di satu tempat.
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Tipe range untuk jam. Postgres punya daterange/tstzrange bawaan, tapi tidak
-- punya range untuk `time`, jadi kita bikin sendiri. Dipakai oleh EXCLUDE
-- constraint di availability_rules.
CREATE TYPE timerange AS RANGE (subtype = time);

CREATE TYPE user_role           AS ENUM ('student', 'lecturer', 'admin');
CREATE TYPE slot_status         AS ENUM ('open', 'booked');
CREATE TYPE booking_status      AS ENUM ('confirmed', 'cancelled', 'completed', 'no_show');
CREATE TYPE notification_status AS ENUM ('pending', 'sent', 'failed');

-- Satu-satunya trigger di database ini. updated_at itu mekanis, bukan logika
-- bisnis — menaruhnya di sini berarti mustahil lupa mengisinya dari Go.
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- ---------------------------------------------------------------------------
-- users
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email           text        NOT NULL,
    password_hash   text        NOT NULL,
    full_name       text        NOT NULL,
    role            user_role   NOT NULL,
    identity_number text,        -- NIM untuk mahasiswa, NIDN untuk dosen, NULL untuk admin
    is_active       boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Unique pada lower(email), bukan pada email. "Budi@kampus.ac.id" dan
-- "budi@kampus.ac.id" adalah orang yang sama; tanpa ini dia bisa punya 2 akun.
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- Partial: admin tidak punya NIM/NIDN. Postgres sebenarnya sudah membolehkan
-- banyak NULL di UNIQUE biasa, tapi ditulis eksplisit supaya niatnya terbaca.
CREATE UNIQUE INDEX users_identity_number_key
    ON users (identity_number) WHERE identity_number IS NOT NULL;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE lecturer_profiles (
    user_id              uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department           text     NOT NULL,
    room                 text,
    bio                  text,
    -- HANYA nilai awal yang mengisi form saat dosen membuat aturan baru.
    -- Generator slot TIDAK PERNAH membaca kolom ini. Sumber kebenaran durasi
    -- ada di availability_rules.slot_duration_min.
    default_duration_min smallint NOT NULL DEFAULT 30
        CHECK (default_duration_min BETWEEN 15 AND 180)
);


-- ---------------------------------------------------------------------------
-- ketersediaan dosen
-- ---------------------------------------------------------------------------
CREATE TABLE availability_rules (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lecturer_id       uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 0 = Minggu .. 6 = Sabtu. Sengaja mengikuti EXTRACT(DOW) Postgres,
    -- time.Weekday Go, dan Date.getDay() JavaScript sekaligus — supaya tidak
    -- ada konversi diam-diam di perbatasan antar bahasa.
    day_of_week       smallint    NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    start_time        time        NOT NULL,
    end_time          time        NOT NULL,
    slot_duration_min smallint    NOT NULL CHECK (slot_duration_min BETWEEN 15 AND 180),
    effective_from    date        NOT NULL,
    effective_to      date,        -- NULL = berlaku selamanya
    is_active         boolean     NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT rule_time_order CHECK (end_time > start_time),
    CONSTRAINT rule_date_order CHECK (effective_to IS NULL OR effective_to >= effective_from),
    -- Rentang 09:00-09:20 dengan slot 30 menit tidak menghasilkan slot apa pun.
    -- Lebih baik ditolak saat dibuat daripada diam-diam kosong.
    CONSTRAINT rule_window_fits_duration CHECK (
        EXTRACT(EPOCH FROM (end_time - start_time)) >= slot_duration_min * 60
    ),

    -- Dosen yang sama, hari yang sama, periode berlaku beririsan DAN jamnya
    -- beririsan -> ditolak. Ini yang mencegah aturan "Senin 09-12" dan
    -- "Senin 10-14" hidup bersamaan lalu menghasilkan slot bertabrakan.
    -- Aturan yang sudah dinonaktifkan tidak ikut dihitung.
    CONSTRAINT no_overlapping_rules EXCLUDE USING gist (
        lecturer_id                                    WITH =,
        day_of_week                                    WITH =,
        daterange(effective_from, effective_to, '[]')  WITH &&,
        timerange(start_time, end_time)                WITH &&
    ) WHERE (is_active)
);

CREATE INDEX availability_rules_lecturer_idx
    ON availability_rules (lecturer_id) WHERE is_active;

CREATE TRIGGER availability_rules_set_updated_at
    BEFORE UPDATE ON availability_rules FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE availability_exceptions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lecturer_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exception_date date        NOT NULL,   -- bukan "date": itu nama tipe, bikin query membingungkan
    reason         text        NOT NULL,
    is_full_day    boolean     NOT NULL DEFAULT true,
    start_time     time,
    end_time       time,
    created_at     timestamptz NOT NULL DEFAULT now(),

    -- Pengecualian hanya punya dua bentuk sah: seharian penuh (tanpa jam), atau
    -- sebagian hari (dengan jam lengkap). Bentuk ketiga tidak bisa disimpan.
    CONSTRAINT exception_shape CHECK (
        (is_full_day AND start_time IS NULL AND end_time IS NULL)
        OR
        (NOT is_full_day AND start_time IS NOT NULL AND end_time IS NOT NULL
         AND end_time > start_time)
    )
);

CREATE INDEX availability_exceptions_lookup_idx
    ON availability_exceptions (lecturer_id, exception_date);


-- ---------------------------------------------------------------------------
-- slot & booking — inti project
-- ---------------------------------------------------------------------------
CREATE TABLE slots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lecturer_id uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    start_at    timestamptz NOT NULL,
    end_at      timestamptz NOT NULL,
    -- Denormalisasi yang disengaja: informasi ini juga tersirat dari tabel
    -- bookings. Disimpan di sini supaya listing slot 30 hari x banyak dosen
    -- cukup membaca satu tabel. Konsekuensinya kolom ini WAJIB diubah di dalam
    -- transaction yang sama dengan perubahan booking-nya, tanpa kecuali.
    status      slot_status NOT NULL DEFAULT 'open',
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT slot_time_order CHECK (end_at > start_at),
    -- Membuat generator slot idempoten: dijalankan dua kali tidak menggandakan.
    CONSTRAINT slots_lecturer_start_key UNIQUE (lecturer_id, start_at)
);
-- Tidak ada index tambahan di sini. Constraint UNIQUE di atas sudah membuat
-- index (lecturer_id, start_at) yang persis dipakai query listing slot.


CREATE TABLE bookings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- RESTRICT, bukan CASCADE: slot yang sudah ada booking-nya tidak boleh
    -- terhapus. Inilah yang membuat "hapus slot open saat dosen cuti" aman.
    slot_id       uuid           NOT NULL REFERENCES slots(id) ON DELETE RESTRICT,
    student_id    uuid           NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    topic         text           NOT NULL,
    description   text,
    status        booking_status NOT NULL DEFAULT 'confirmed',
    lecturer_note text,
    cancelled_at  timestamptz,
    cancelled_by  uuid           REFERENCES users(id),   -- mahasiswa, dosen, atau sistem saat cuti
    cancel_reason text,
    created_at    timestamptz    NOT NULL DEFAULT now(),
    updated_at    timestamptz    NOT NULL DEFAULT now(),

    CONSTRAINT cancel_fields_consistent CHECK (
        (status = 'cancelled'  AND cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL)
        OR
        (status <> 'cancelled' AND cancelled_at IS NULL)
    )
);

-- JARING PENGAMAN ANTI DOUBLE-BOOKING.
-- `<> 'cancelled'`, bukan `= 'confirmed'`: booking yang sudah 'completed' atau
-- 'no_show' tetap menduduki slotnya. Kalau memakai `= 'confirmed'`, sesi yang
-- ditandai selesai akan keluar dari index dan slotnya "bebas" lagi.
-- Hanya pembatalan yang membebaskan slot.
CREATE UNIQUE INDEX one_active_booking_per_slot
    ON bookings (slot_id) WHERE status <> 'cancelled';

-- Index penuh (termasuk yang cancelled), untuk join dashboard dosen dan
-- riwayat yang ikut menampilkan booking batal.
CREATE INDEX bookings_slot_idx    ON bookings (slot_id);
CREATE INDEX bookings_student_idx ON bookings (student_id, created_at DESC);

CREATE TRIGGER bookings_set_updated_at
    BEFORE UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ---------------------------------------------------------------------------
-- auth
-- ---------------------------------------------------------------------------
CREATE TABLE refresh_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 mentah, bukan bcrypt. Bcrypt sengaja lambat untuk melindungi
    -- password yang entropinya rendah; refresh token itu 32 byte acak, tidak
    -- bisa ditebak, jadi hash lambat cuma memperlambat setiap refresh.
    token_hash  bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    -- Diisi saat token dirotasi. Kalau token yang sudah punya penerus dipakai
    -- lagi, artinya ada yang mencuri salinannya -> cabut seluruh rantainya.
    replaced_by uuid        REFERENCES refresh_tokens(id) ON DELETE SET NULL,
    user_agent  text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX refresh_tokens_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_active_idx
    ON refresh_tokens (user_id) WHERE revoked_at IS NULL;


-- ---------------------------------------------------------------------------
-- operasional
-- ---------------------------------------------------------------------------
-- Kenapa tabel ini ada padahal antreannya sudah di Redis (asynq):
-- Redis adalah alat PENGIRIM, tabel ini adalah CATATAN. Kalau Redis di-flush
-- atau restart tanpa persistence, antreannya hilang dan tidak ada yang tahu
-- reminder mana yang belum terkirim. Pola outbox: tulis niat ke Postgres di
-- transaction yang sama dengan booking-nya, worker yang mengeksekusi.
CREATE TABLE notifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid                NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    booking_id   uuid                REFERENCES bookings(id) ON DELETE CASCADE,
    type         text                NOT NULL,   -- 'booking_reminder', 'booking_cancelled', ...
    payload      jsonb               NOT NULL DEFAULT '{}'::jsonb,
    scheduled_at timestamptz         NOT NULL,
    sent_at      timestamptz,
    status       notification_status NOT NULL DEFAULT 'pending',
    attempts     smallint            NOT NULL DEFAULT 0,
    last_error   text,
    created_at   timestamptz         NOT NULL DEFAULT now()
);

-- Satu booking hanya boleh punya satu reminder H-1. Tanpa ini, worker yang
-- dijalankan dua kali (atau dua instance worker) mengirim email dobel.
CREATE UNIQUE INDEX notifications_dedupe_key
    ON notifications (booking_id, type) WHERE booking_id IS NOT NULL;

CREATE INDEX notifications_due_idx
    ON notifications (scheduled_at) WHERE status = 'pending';


-- Idempotency: mencegah double-submit dari tombol yang diklik dua kali atau
-- retry otomatis di jaringan buruk.
-- Alurnya: INSERT dulu untuk "mengklaim" key (baris ini yang jadi lock-nya).
--   - konflik + completed_at terisi -> kembalikan response_body yang tersimpan
--   - konflik + completed_at NULL   -> 409, request kembarannya masih berjalan
--   - key sama tapi request_hash beda -> 422, key dipakai ulang untuk isi lain
-- Idempoten yang benar bukan sekadar menolak duplikat, tapi mengembalikan
-- jawaban yang SAMA PERSIS seperti request pertama.
CREATE TABLE idempotency_keys (
    user_id       uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key           text        NOT NULL,
    endpoint      text        NOT NULL,
    request_hash  text        NOT NULL,
    status_code   smallint,
    response_body jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz,

    PRIMARY KEY (user_id, key)
);

CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
