package booking

import (
	"context"
	"errors"
	"fmt"

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
	pool      *pgxpool.Pool
	maxActive int
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, maxActive: DefaultMaxActiveBookings}
}

// Create memesan satu slot untuk satu mahasiswa.
//
// URUTAN PENGUNCIAN DI SINI ADALAH BAGIAN TERPENTING DARI SELURUH PROJECT.
// Jangan diubah tanpa membaca komentar di tiap langkah.
//
// Ringkasnya:
//  1. kunci baris mahasiswa  -> menegakkan batas 3 booking aktif
//  2. kunci baris slot       -> menegakkan anti double-booking
//  3. hitung booking aktif
//  4. simpan booking         -> partial unique index sebagai jaring pengaman
//  5. tandai slot 'booked'
//
// Urutan 1 lalu 2 selalu sama untuk SEMUA pemanggil. Itulah yang mencegah
// deadlock: deadlock terjadi kalau dua transaksi mengambil dua kunci yang sama
// dalam urutan terbalik, dan di sini urutan terbalik tidak pernah mungkin.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Booking, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("memulai transaction: %w", err)
	}
	// Rollback setelah Commit adalah no-op di pgx, jadi defer ini aman dipasang
	// tanpa syarat. Gunanya: jalur error mana pun di bawah — termasuk yang
	// belum ada saat kode ini ditulis — tidak akan pernah meninggalkan
	// transaction menggantung yang memegang kunci baris selamanya.
	defer tx.Rollback(ctx)

	// LANGKAH 1 — kunci baris mahasiswa.
	//
	// Kenapa ini perlu padahal yang diperebutkan adalah slot? Karena batas
	// "maksimal 3 booking aktif" dihitung dengan COUNT di langkah 3. Kalau satu
	// mahasiswa menembakkan 10 booking ke 10 slot BERBEDA sekaligus, kunci slot
	// tidak menolong sama sekali — slotnya beda-beda, tidak ada yang berebut.
	// Kesepuluh transaksi menghitung "aktif = 0" bersamaan, lalu kesepuluhnya
	// menyimpan. Batas 3 jebol jadi 10.
	//
	// FOR NO KEY UPDATE, bukan FOR UPDATE. Bedanya halus tapi nyata: saat
	// menyimpan booking, Postgres otomatis mengambil kunci FOR KEY SHARE pada
	// baris users karena ada foreign key student_id. FOR UPDATE berbenturan
	// dengan FOR KEY SHARE, jadi memakainya akan memblokir transaksi lain yang
	// sebenarnya tidak ada urusan dengan mahasiswa ini. FOR NO KEY UPDATE tetap
	// berbenturan dengan sesama FOR NO KEY UPDATE — yang persis kita butuhkan
	// untuk menserialkan booking milik mahasiswa yang sama — tanpa mengganggu
	// pemeriksaan foreign key milik orang lain.
	var satu int
	err = tx.QueryRow(ctx,
		`SELECT 1 FROM users WHERE id = $1::uuid FOR NO KEY UPDATE`,
		in.StudentID,
	).Scan(&satu)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStudentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengunci baris mahasiswa: %w", err)
	}

	// LANGKAH 2 — kunci baris slot.
	//
	// FOR UPDATE membuat transaksi lain yang meminta baris ini MENUNGGU, bukan
	// gagal. Jadi request kedua tidak langsung ditolak; ia menunggu sampai
	// request pertama commit, lalu membaca status yang sudah berubah menjadi
	// 'booked' dan mendapat penolakan yang deterministik.
	//
	// Kenapa row-level lock Postgres, bukan distributed lock di Redis:
	//   - sumber kebenarannya ada di database, jadi kuncinya sebaiknya juga
	//   - kunci ini hidup dan mati bersama transaction; kalau proses ini crash
	//     di tengah, Postgres melepasnya sendiri. Lock di Redis butuh TTL, dan
	//     TTL yang kependekan melepas kunci saat pekerjaan belum selesai.
	//   - tidak menambah komponen yang harus ikut hidup agar booking benar.
	//
	// Satu query ini mengambil status dan cek withdrawn sekaligus — lebih
	// efisien dari pada query terpisah, dan konsistensinya tetap terjaga
	// karena keduanya dilindungi FOR UPDATE yang sama.
	var status string
	var withdrawn bool
	err = tx.QueryRow(ctx,
		`SELECT status::text, withdrawn_at IS NOT NULL FROM slots WHERE id = $1::uuid FOR UPDATE`,
		in.SlotID,
	).Scan(&status, &withdrawn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSlotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mengunci baris slot: %w", err)
	}
	if withdrawn {
		return nil, ErrSlotWithdrawn
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
		// errors.As menelusuri rantai error sampai menemukan tipe yang dicari.
		// Ini yang membuat pembungkusan dengan %w di seluruh file ini berguna:
		// error asli dari driver tidak pernah hilang, cuma terbungkus.
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

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &b, nil
}
