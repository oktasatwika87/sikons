// Package booking berisi logika pemesanan slot konsultasi.
//
// Di sinilah inti project ini berada: mencegah dua mahasiswa memesan slot yang
// sama. Seluruh keputusan di package ini berputar di sekitar satu pertanyaan —
// apa yang terjadi kalau dua request tiba pada milidetik yang sama.
package booking

import (
	"errors"
	"time"
)

// DefaultMaxActiveBookings membatasi berapa banyak booking mendatang yang boleh
// dipegang satu mahasiswa sekaligus. Tanpa batas ini, satu orang bisa memborong
// seluruh jadwal seorang dosen.
const DefaultMaxActiveBookings = 3

// Error domain. Semuanya dideklarasikan sebagai variabel supaya pemanggil bisa
// membedakannya dengan errors.Is(), bukan dengan mencocokkan pesan.
//
// Kenapa ini penting: layer HTTP nanti perlu memetakan ErrSlotAlreadyBooked ke
// 409 dan ErrSlotNotFound ke 404. Kalau pembedanya adalah string pesan, maka
// memperbaiki typo di pesan error diam-diam mengubah status code API.
var (
	ErrSlotNotFound            = errors.New("slot tidak ditemukan")
	ErrSlotAlreadyBooked      = errors.New("slot sudah dipesan")
	ErrSlotWithdrawn          = errors.New("slot sudah tidak tersedia")
	ErrStudentNotFound        = errors.New("mahasiswa tidak ditemukan atau tidak aktif")
	ErrLimitReached           = errors.New("batas booking aktif tercapai")
	ErrSlotTooSoon            = errors.New("jarak tempuh booking terlalu pendek")
	ErrIdempotencyReused      = errors.New("idempotency key dipakai ulang dengan isi berbeda")
	ErrBookingNotFound        = errors.New("booking tidak ditemukan")
	ErrBookingAlreadyCancelled = errors.New("booking sudah dibatalkan")
	ErrBookingAlreadyFinalized = errors.New("booking sudah diselesaikan")
	ErrCancelTooLate          = errors.New("pembatalan terlalu dekat dengan jadwal")
	ErrBookingNotConfirmed     = errors.New("booking belum dikonfirmasi")
	ErrSessionNotStarted      = errors.New("sesi belum dimulai")
)

type CreateInput struct {
	SlotID         string
	StudentID      string
	Topic          string
	Description    string
	IdempotencyKey string
	RequestHash    string
}

// Booking adalah representasi minimum satu baris di tabel bookings.
// Untuk view lengkap (dengan info slot dan person), pakai BookingView.
type Booking struct {
	ID          string
	SlotID      string
	StudentID   string
	Topic       string
	Description string
	Status      string
	CreatedAt   time.Time
}

// BookingView adalah join lengkap dari bookings + slots + users + lecturer_profiles.
// Service mengembalikannya agar HTTP handler tidak perlu query sendiri — dengan
// begitu package server tidak perlu tahu SQL apa pun.
type BookingView struct {
	ID          string    `json:"id"`
	SlotID      string    `json:"slot_id"`
	StudentID   string    `json:"student_id"`
	LecturerID  string    `json:"lecturer_id"`
	Topic       string    `json:"topic"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	SlotStart   time.Time `json:"slot_start_at"`
	SlotEnd     time.Time `json:"slot_end_at"`

	// Person — hanya salah satu yang terisi, tergantung peran pemanggil.
	LecturerFullName   string
	LecturerDepartment string
	StudentFullName    string
	StudentIdentity    string
}

// CreateResult adalah keluaran service.Create. Body berisi JSON yang SUDAH jadi
// — siap ditulis ke ResponseWriter. Replay = true bila body berasal dari
// idempotency_keys, false bila baru saja di-generate.
type CreateResult struct {
	Booking *Booking
	Status  int
	Body    []byte
	Replay  bool
}
