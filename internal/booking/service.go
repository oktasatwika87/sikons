package booking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service memakai *pgxpool.Pool secara langsung, BUKAN lewat interface.
//
// Ini kebalikan dari yang saya lakukan di package server, dan itu disengaja.
// Aturannya: pakai interface untuk hal yang ingin kamu PALSUKAN, pakai yang
// asli untuk hal yang ingin kamu BUKTIKAN. Yang sedang diuji di sini adalah
// perilaku penguncian baris Postgres — database palsu tidak membuktikan
// apa pun tentang itu.
type Service struct {
	pool           *pgxpool.Pool
	maxActive      int
	minLeadMinutes int
}

func NewService(pool *pgxpool.Pool, minLeadMinutes int) *Service {
	return &Service{
		pool:           pool,
		maxActive:      DefaultMaxActiveBookings,
		minLeadMinutes: minLeadMinutes,
	}
}

// MinLeadMinutes mengembalikan minimal menit sebelum slot yang dipakai service
// saat memutuskan ErrSlotTooSoon. Handler HTTP membacanya untuk membuat pesan
// error yang akurat tanpa hardcode angka.
func (s *Service) MinLeadMinutes() int {
	return s.minLeadMinutes
}

// Create memesan satu slot untuk satu mahasiswa.
//
// Urutan di dalam transaction TIDAK BOLEH diubah. Berikut alasannya:
//
// LANGKAH 0 (idempotency — SEBELUM kunci apapun):
//
//	Klaim idempotency key SEBELUM kunci baris. Kalau diletakkan di akhir,
//	request kembar sudah keburu kalah di kunci slot dan menerima
//	ErrSlotAlreadyBooked — persis yang mau dicegah.
//
//	ON CONFLICT DO NOTHING terhadap transaksi kembar yang belum commit membuat
//	Postgres MENUNGGU, bukan mengembalikan nol baris. Transaction yang menang
//	commit duluan, transaction yang kalah bangun dari wait dan INSERT-nya
//	mendapat constraint violation -> dapat nol baris -> masuk ke replay path.
//	Ini yang menyerialkan request kembar tanpa perlu lock terpisah.
//
//	Satu transaction: crash di tengah = rollback = key hilang = retry bersih.
//	Pola dua fase (INSERT klaim, COMMIT, baru INSERT booking) meninggalkan
//	klaim yatim kalau crash sebelum booking disimpan; dua transaction juga
//	menambah latency dan kompleksitas reaper untuk klaim basi.
//
// LANGKAH 1 — kunci baris mahasiswa.
// LANGKAH 2 — kunci baris slot.
// LANGKAH 3 — hitung booking aktif.
// LANGKAH 4 — simpan booking.
// LANGKAH 5 — tandai slot booked.
// LANGKAH 6 — tulis response_body ke idempotency_keys.
//
// Urutan 1 lalu 2 selalu sama untuk SEMUA pemanggil. Itulah yang mencegah
// deadlock: deadlock terjadi kalau dua transaksi mengambil dua kunci yang sama
// dalam urutan terbalik, dan di sini urutan terbalik tidak pernah mungkin.
func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("memulai transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// LANGKAH 0 — klaim idempotency key.
	//
	// Kalau key kosong, lewati seluruh mekanisme ini.
	if in.IdempotencyKey != "" {
		var keyAda bool
		err = tx.QueryRow(ctx, `
			INSERT INTO idempotency_keys (user_id, key, endpoint, request_hash)
			VALUES ($1::uuid, $2, '/api/v1/bookings', $3)
			ON CONFLICT (user_id, key) DO NOTHING
			RETURNING true`,
			in.StudentID, in.IdempotencyKey, in.RequestHash,
		).Scan(&keyAda)
		if errors.Is(err, pgx.ErrNoRows) {
			// Key sudah ada. Cek apakah isinya sama dan sudah selesai.
			var storedEndpoint, storedHash string
			var statusCode *int
			var responseBody []byte
			err = tx.QueryRow(ctx, `
				SELECT endpoint, request_hash, status_code, response_body::text::bytea
				FROM idempotency_keys
				WHERE user_id = $1::uuid AND key = $2`,
				in.StudentID, in.IdempotencyKey,
			).Scan(&storedEndpoint, &storedHash, &statusCode, &responseBody)
			if err != nil {
				return nil, fmt.Errorf("membaca idempotency key: %w", err)
			}
			// endpoint harus sama — key yang sama tidak boleh dipakai di endpoint lain.
			if storedEndpoint != "/api/v1/bookings" {
				return nil, ErrIdempotencyReused
			}
			// request_hash berbeda = key dipakai ulang dengan isi berbeda.
			if storedHash != in.RequestHash {
				return nil, ErrIdempotencyReused
			}
			// Belum selesai? Sama dengan hash cocok tapi completed_at masih NULL —
			// artinya transaksi sebelumnya sedang berjalan atau rollback.
			// Karena Postgres menunggu di INSERT ON CONFLICT, baris ini sebenarnya
			// hanya muncul kalau transaksi sebelumnya sudah commit (kalau belum,
			// kita masih menunggu di atas). Jadi kalau completed_at NULL di sini,
			// itu anomali: catat dan laporkan supaya bisa diselidiki.
			if statusCode == nil {
				return nil, fmt.Errorf("idempotency key '%s' ada tapi belum selesai", in.IdempotencyKey)
			}
			// Sumber kebenaran untuk replay adalah response_body yang tersimpan,
			// BUKAN query ulang ke tabel bookings. Kalau mahasiswa pernah
			// membatalkan lalu memesan lagi, query ulang akan mengembalikan
			// booking yang SALAH.
			return &CreateResult{
				Booking: nil,
				Status:  *statusCode,
				Body:    responseBody,
				Replay:  true,
			}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("menklaim idempotency key: %w", err)
		}
		// keyAda == true: klaim berhasil, lanjut ke langkah 1.
	}

	// LANGKAH 1 — kunci baris mahasiswa.
	//
	// Tiga hal dicek sekaligus di sini:
	//   - baris ada
	//   - is_active = true  (akun tidak dinonaktifkan)
	//   - role = 'student' (bukan dosen/admin)
	// Semua dalam satu FOR NO KEY UPDATE sehingga konsisten dan efisien.
	//
	// FOR NO KEY UPDATE, bukan FOR UPDATE. Bedanya halus tapi nyata: saat
	// menyimpan booking, Postgres otomatis mengambil kunci FOR KEY SHARE pada
	// baris users karena ada foreign key student_id. FOR UPDATE berbenturan
	// dengan FOR KEY SHARE, jadi memakainya akan memblokir transaksi lain yang
	// sebenarnya tidak ada urusan dengan mahasiswa ini. FOR NO KEY UPDATE tetap
	// berbenturan dengan sesama FOR NO KEY UPDATE — yang persis kita butuhkan
	// untuk menserialkan booking milik mahasiswa yang sama — tanpa mengganggu
	// pemeriksaan foreign key milik orang lain.
	//
	// ErrStudentNotFound sekarang berarti: token menunjuk user yang sudah
	// tidak ada, nonaktif, atau bukan mahasiswa. Di HTTP layer, ini dikembalikan
	// sebagai 401 (token valid tapi akun tidak valid).
	var studentOK bool
	err = tx.QueryRow(ctx, `
		SELECT true FROM users
		WHERE id = $1::uuid AND is_active AND role = 'student'
		FOR NO KEY UPDATE`,
		in.StudentID,
	).Scan(&studentOK)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStudentNotFound
	}
	if !studentOK {
		return nil, ErrStudentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengunci baris mahasiswa: %w", err)
	}

	// LANGKAH 2 — kunci baris slot.
	//
	// Tiga hal dicek sekaligus: status, withdrawn, dan jarak tempuh.
	// Pemeriksaan jarak tempuh pakai now() Postgres, BUKAN time.Now() Go —
	// sumber jam yang berbeda (app server vs database) bisa menghasilkan
	// perbedaan hasil saat beban tinggi atau zona waktu tidak sinkron.
	//
	// IF dengan make_interval(), BUKAN parameter Go, menjamin kondisi ini
	// selalu dievaluasi oleh Postgres pada saat execution, bukan planning.
	var status string
	var withdrawn bool
	var tooSoon bool
	err = tx.QueryRow(ctx, `
		SELECT status::text,
		       withdrawn_at IS NOT NULL,
		       start_at <= now() + make_interval(mins => $2)
		FROM slots WHERE id = $1::uuid FOR UPDATE`,
		in.SlotID, s.minLeadMinutes,
	).Scan(&status, &withdrawn, &tooSoon)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSlotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengunci baris slot: %w", err)
	}
	if withdrawn {
		return nil, ErrSlotWithdrawn
	}
	if tooSoon {
		return nil, ErrSlotTooSoon
	}
	if status != "open" {
		return nil, ErrSlotAlreadyBooked
	}

	// LANGKAH 3 — hitung booking aktif milik mahasiswa ini.
	//
	// Hanya yang belum lewat yang dihitung: konsultasi minggu lalu tidak boleh
	// menahan jatah minggu ini.
	var aktif int
	err = tx.QueryRow(ctx, `
		SELECT count(*)
		FROM bookings b
		JOIN slots s ON s.id = b.slot_id
		WHERE b.student_id = $1::uuid
		  AND b.status = 'confirmed'
		  AND s.start_at > now()`,
		in.StudentID,
	).Scan(&aktif)
	if err != nil {
		return nil, fmt.Errorf("menghitung booking aktif: %w", err)
	}
	if aktif >= s.maxActive {
		return nil, ErrLimitReached
	}

	// LANGKAH 4 — simpan booking.
	//
	// Secara logika, sampai di sini slot sudah dikunci dan statusnya sudah
	// dipastikan 'open', jadi INSERT ini tidak mungkin bentrok. Penanganan
	// error 23505 di bawah tetap ditulis sebagai JARING PENGAMAN: kalau suatu
	// hari ada kode lain yang menyimpan booking tanpa melewati langkah 2 —
	// migrasi data, skrip perbaikan, endpoint baru yang ditulis buru-buru —
	// partial unique index one_active_booking_per_slot yang akan menghentikannya,
	// dan pengguna tetap menerima 409 yang benar, bukan 500.
	var b Booking
	var deskripsi *string
	if in.Description != "" {
		deskripsi = &in.Description
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO bookings (slot_id, student_id, topic, description)
		VALUES ($1::uuid, $2::uuid, $3, $4)
		RETURNING id::text, slot_id::text, student_id::text,
		          topic, coalesce(description, ''), status::text, created_at`,
		in.SlotID, in.StudentID, in.Topic, deskripsi,
	).Scan(&b.ID, &b.SlotID, &b.StudentID, &b.Topic, &b.Description, &b.Status, &b.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "one_active_booking_per_slot" {
			return nil, ErrSlotAlreadyBooked
		}
		return nil, fmt.Errorf("menyimpan booking: %w", err)
	}

	// LANGKAH 5 — tandai slotnya terpakai.
	//
	// `AND status = 'open'` sebenarnya sudah dijamin oleh langkah 2. Ditulis
	// tetap, lalu jumlah baris terdampak diperiksa: kalau suatu saat asumsinya
	// runtuh, transaction ini gagal dengan berisik alih-alih diam-diam
	// meninggalkan slot dan booking dalam keadaan tidak sinkron.
	ct, err := tx.Exec(ctx,
		`UPDATE slots SET status = 'booked' WHERE id = $1::uuid AND status = 'open'`,
		in.SlotID,
	)
	if err != nil {
		return nil, fmt.Errorf("menandai slot terpakai: %w", err)
	}
	if ct.RowsAffected() != 1 {
		return nil, fmt.Errorf("slot %s gagal ditandai terpakai: %d baris terdampak",
			in.SlotID, ct.RowsAffected())
	}

	// LANGKAH 6 — tulis response_body ke idempotency_keys.
	//
	// Body yang disimpan di sini adalah yang AKAN dikembalikan ke klien. Saat
	// replay, body ini dibaca ulang apa adanya — itulah yang menjamin replay
	// mengembalikan id booking yang PERTAMA, bukan id terbaru kalau mahasiswa
	// sempat membatalkan dan memesan lagi.
	body, err := json.Marshal(map[string]string{"id": b.ID})
	if err != nil {
		return nil, fmt.Errorf("menyusun response body: %w", err)
	}

	if in.IdempotencyKey != "" {
		_, err = tx.Exec(ctx, `
			UPDATE idempotency_keys
			SET status_code = 201,
			    response_body = $1::jsonb,
			    completed_at = now()
			WHERE user_id = $2::uuid AND key = $3`,
			string(body), in.StudentID, in.IdempotencyKey,
		)
		if err != nil {
			return nil, fmt.Errorf("menandai idempotency selesai: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &CreateResult{
		Booking: &b,
		Status:  http.StatusCreated,
		Body:    body,
		Replay:  false,
	}, nil
}

// GetByID mengambil satu booking. Kalau requester tidak punya akses (bukan
// pemilik dan bukan dosen di slot itu), ErrBookingNotFound dikembalikan —
// supaya kita tidak bocorin informasi "ada tapi bukan milikmu" lewat 403.
//
// viewRole menentukan field person mana yang diisi: "student" -> lecturer
// fields, "lecturer" -> student fields. Role lain -> "" semua.
func (s *Service) GetByID(ctx context.Context, bookingID, requesterID, requesterRole string) (*BookingView, error) {
	view, err := s.loadBookingView(ctx, `WHERE b.id = $1::uuid`, bookingID)
	if err != nil {
		return nil, err
	}

	switch requesterRole {
	case "student":
		if view.StudentID != requesterID {
			return nil, ErrBookingNotFound
		}
	case "lecturer":
		if view.LecturerID != requesterID {
			return nil, ErrBookingNotFound
		}
	default:
		return nil, ErrBookingNotFound
	}
	return view, nil
}

// ListByStudent mengambil booking milik satu mahasiswa dengan pagination.
func (s *Service) ListByStudent(ctx context.Context, studentID, status string, page, perPage int) ([]BookingView, int, error) {
	return listBookingsView(ctx, s.pool, "student", studentID, status, page, perPage)
}

// ListByLecturer mengambil booking di slot milik satu dosen dengan pagination.
func (s *Service) ListByLecturer(ctx context.Context, lecturerID, status string, page, perPage int) ([]BookingView, int, error) {
	return listBookingsView(ctx, s.pool, "lecturer", lecturerID, status, page, perPage)
}

// loadBookingView menjalankan query join untuk satu booking. Dipakai oleh GetByID.
// Kalau lebih dari satu view atau nol, mengembalikan ErrBookingNotFound.
func (s *Service) loadBookingView(ctx context.Context, where string, args ...any) (*BookingView, error) {
	q := `
		SELECT b.id::text, b.slot_id::text, b.student_id::text,
		       sl.lecturer_id::text,
		       b.topic, coalesce(b.description, ''), b.status::text, b.created_at,
		       sl.start_at, sl.end_at,
		       u.full_name, coalesce(lp.department, '') AS department,
		       '' AS student_full_name, '' AS student_identity
		FROM bookings b
		JOIN slots sl ON sl.id = b.slot_id
		JOIN users u ON u.id = sl.lecturer_id
		LEFT JOIN lecturer_profiles lp ON lp.user_id = u.id
		` + where

	var v BookingView
	err := s.pool.QueryRow(ctx, q, args...).Scan(
		&v.ID, &v.SlotID, &v.StudentID, &v.LecturerID,
		&v.Topic, &v.Description, &v.Status, &v.CreatedAt,
		&v.SlotStart, &v.SlotEnd,
		&v.LecturerFullName, &v.LecturerDepartment,
		&v.StudentFullName, &v.StudentIdentity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBookingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengambil booking: %w", err)
	}

	// Isi field student kalau tersedia (untuk view dosen).
	if v.StudentID != "" {
		var fullName, identity string
		err := s.pool.QueryRow(ctx, `
			SELECT full_name, coalesce(identity_number, '')
			FROM users WHERE id = $1::uuid`, v.StudentID,
		).Scan(&fullName, &identity)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("mengambil data mahasiswa: %w", err)
		}
		v.StudentFullName = fullName
		v.StudentIdentity = identity
	}

	return &v, nil
}

func listBookingsView(ctx context.Context, pool *pgxpool.Pool, role, userID, status string, page, perPage int) ([]BookingView, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	offset := (page - 1) * perPage

	var args []any
	var argIdx int

	// Bangun where clause berdasarkan peran.
	var whereClause string
	switch role {
	case "student":
		args = append(args, userID)
		argIdx = 2
		whereClause = "b.student_id = $1"
	case "lecturer":
		args = append(args, userID)
		argIdx = 2
		whereClause = "sl.lecturer_id = $1"
	default:
		// Role tidak dikenal — kembalikan empty list dengan total 0, BUKAN
		// meloloskan whereClause kosong yang akan membaca SELURUH tabel.
		return nil, 0, nil
	}

	if status != "" {
		whereClause += fmt.Sprintf(" AND b.status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	// Hitung total.
	var total int
	countQuery := fmt.Sprintf(`SELECT count(*) FROM bookings b JOIN slots sl ON sl.id = b.slot_id WHERE %s`, whereClause)
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("menghitung booking: %w", err)
	}

	// Ambil halaman.
	args = append(args, perPage, offset)
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		SELECT b.id::text, b.slot_id::text, b.student_id::text,
		       sl.lecturer_id::text,
		       b.topic, coalesce(b.description, ''), b.status::text, b.created_at,
		       sl.start_at, sl.end_at,
		       u.full_name, coalesce(lp.department, '') AS department,
		       '' AS student_full_name, '' AS student_identity
		FROM bookings b
		JOIN slots sl ON sl.id = b.slot_id
		JOIN users u ON u.id = sl.lecturer_id
		LEFT JOIN lecturer_profiles lp ON lp.user_id = u.id
		WHERE %s
		ORDER BY sl.start_at DESC
		LIMIT $%d OFFSET $%d`,
		whereClause, argIdx, argIdx+1),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("mengambil booking: %w", err)
	}
	defer rows.Close()

	var bookings []BookingView
	for rows.Next() {
		var v BookingView
		if err := rows.Scan(
			&v.ID, &v.SlotID, &v.StudentID, &v.LecturerID,
			&v.Topic, &v.Description, &v.Status, &v.CreatedAt,
			&v.SlotStart, &v.SlotEnd,
			&v.LecturerFullName, &v.LecturerDepartment,
			&v.StudentFullName, &v.StudentIdentity,
		); err != nil {
			return nil, 0, fmt.Errorf("membaca booking: %w", err)
		}
		bookings = append(bookings, v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("membaca hasil booking: %w", err)
	}

	// Untuk view list, hanya dosen yang butuh field student — dan query kedua
	// di sini merugikan kalau pemanggilnya mahasiswa (yang tidak butuh).
	// Saat ini hemat saja: kalau ada booking dan role adalah lecturer, isi
	// field student dengan satu query batched.
	if role == "lecturer" && len(bookings) > 0 {
		studentIDs := make([]string, 0, len(bookings))
		seen := make(map[string]bool, len(bookings))
		for _, b := range bookings {
			if !seen[b.StudentID] {
				studentIDs = append(studentIDs, b.StudentID)
				seen[b.StudentID] = true
			}
		}
		students, err := loadStudents(ctx, pool, studentIDs)
		if err != nil {
			return nil, 0, err
		}
		for i := range bookings {
			if s, ok := students[bookings[i].StudentID]; ok {
				bookings[i].StudentFullName = s.FullName
				bookings[i].StudentIdentity = s.Identity
			}
		}
	}

	return bookings, total, nil
}

type studentInfo struct {
	FullName string
	Identity string
}

func loadStudents(ctx context.Context, pool *pgxpool.Pool, ids []string) (map[string]studentInfo, error) {
	if len(ids) == 0 {
		return map[string]studentInfo{}, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, full_name, coalesce(identity_number, '')
		FROM users WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("mengambil data mahasiswa: %w", err)
	}
	defer rows.Close()

	out := make(map[string]studentInfo, len(ids))
	for rows.Next() {
		var id, name, identity string
		if err := rows.Scan(&id, &name, &identity); err != nil {
			return nil, fmt.Errorf("membaca data mahasiswa: %w", err)
		}
		out[id] = studentInfo{FullName: name, Identity: identity}
	}
	return out, rows.Err()
}

// ComputeRequestHash menghitung SHA-256 hex dari field request yang sudah
// diurutkan secara deterministik: slot_id + newline + topic + newline + description.
func ComputeRequestHash(slotID, topic, description string) string {
	data := slotID + "\n" + topic + "\n" + description
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}
