package auth

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// Cost 12, bukan default bcrypt (10).
//
// Angka ini eksponensial: tiap +1 melipatduakan waktu hashing. 12 berarti
// sekitar 200-300 ms per hash di laptop modern — cukup lambat untuk membuat
// serangan tebak-menebak massal tidak ekonomis, masih cukup cepat untuk login
// yang terasa instan. Naikkan seiring hardware makin cepat.
var bcryptCost = 12

// bcrypt DIAM-DIAM MEMOTONG password di 72 byte. Password 100 karakter dan 80
// karakter pertamanya sama akan dianggap identik. Karena itu ditolak di depan,
// bukan dibiarkan lewat dan menyisakan kejutan.
const maxPasswordBytes = 72

const minPasswordBytes = 8

// SetBcryptCostUntukTest menurunkan biaya hashing.
//
// Cost 12 memakan ~250 ms per hash. Itu memang tujuannya di produksi — membuat
// serangan tebak-menebak massal tidak ekonomis. Tapi di test, satu suite yang
// menghash 40 password jadi memakan 10 detik tanpa membuktikan apa pun
// tambahan: yang diuji adalah "hash cocok / tidak cocok", bukan "berapa lama".
//
// HANYA untuk test. Namanya sengaja panjang dan canggung supaya tidak ada yang
// memanggilnya di kode produksi tanpa sadar.
func SetBcryptCostUntukTest(cost int) { bcryptCost = cost }

// hashUmpan dipakai saat email tidak ditemukan — lihat BuangWaktuSetaraVerifikasi.
//
// Dihitung saat runtime, bukan ditulis sebagai konstanta. Hash bcrypt yang
// diketik manual gampang salah satu karakter, dan kalau formatnya rusak,
// bcrypt gagal seketika alih-alih membakar waktu — perlindungannya hilang
// tanpa suara. Dihitung sendiri, dia dijamin sah.
//
// sync.OnceValue menghitungnya paling banyak sekali, saat pertama dibutuhkan,
// dan aman dipanggil dari banyak goroutine sekaligus.
var hashUmpan = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("kata sandi umpan"), bcryptCost)
	if err != nil {
		// Kalau bcrypt gagal menghash string konstan, ada yang sangat salah
		// dengan lingkungannya dan aplikasi tidak layak jalan.
		panic("tidak bisa membuat hash umpan: " + err.Error())
	}
	return h
})

// Hangatkan menghitung hash umpan di depan, saat startup.
//
// Tanpa ini, login gagal yang PERTAMA kali akan memakan waktu dua kali lipat
// (menghitung hash umpan + membandingkan), dan justru itu yang menciptakan
// selisih waktu yang ingin kita hilangkan.
func Hangatkan() { _ = hashUmpan() }

func HashPassword(plain string) (string, error) {
	if len(plain) < minPasswordBytes {
		return "", ErrPasswordTooShort
	}
	if len(plain) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(h), nil
}

// VerifyPassword mengembalikan nil kalau cocok, ErrInvalidCredentials kalau
// tidak. Perhatikan: yang dikembalikan BUKAN "password salah" — pesannya
// digabung dengan "email tidak ditemukan" secara sengaja, supaya penyerang
// tidak bisa memakai halaman login untuk mendata email mana yang terdaftar.
func VerifyPassword(hash, plain string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	if err == nil {
		return nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("memeriksa password: %w", err)
}

// BuangWaktuSetaraVerifikasi dipanggil saat email TIDAK ditemukan.
//
// Tanpa ini, login dengan email yang tidak terdaftar akan membalas jauh lebih
// cepat daripada login dengan email terdaftar tapi password salah — karena
// yang pertama tidak pernah menjalankan bcrypt. Selisih ratusan milidetik itu
// cukup untuk dipakai mendata email mana yang punya akun. Pesan errornya sudah
// disamakan; waktunya juga harus.
func BuangWaktuSetaraVerifikasi() {
	_ = bcrypt.CompareHashAndPassword(hashUmpan(), []byte("bukan password siapa pun"))
}
