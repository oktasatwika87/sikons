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
	ErrSlotNotFound      = errors.New("slot tidak ditemukan")
	ErrSlotAlreadyBooked = errors.New("slot sudah dipesan")
	ErrSlotWithdrawn     = errors.New("slot sudah tidak tersedia")
	ErrStudentNotFound   = errors.New("mahasiswa tidak ditemukan")
	ErrLimitReached      = errors.New("batas booking aktif tercapai")
)

type CreateInput struct {
	SlotID      string
	StudentID   string
	Topic       string
	Description string
}

type Booking struct {
	ID          string
	SlotID      string
	StudentID   string
	Topic       string
	Description string
	Status      string
	CreatedAt   time.Time
}
