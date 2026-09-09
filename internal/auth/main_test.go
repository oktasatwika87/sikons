package auth

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain berjalan sekali sebelum seluruh test di package ini.
//
// Biaya bcrypt diturunkan ke minimum supaya suite ini selesai dalam hitungan
// milidetik, bukan puluhan detik. Yang diuji di sini adalah logika — hash
// cocok atau tidak, salt bekerja atau tidak — dan logika itu identik di cost
// berapa pun. Angkanya sendiri sudah diuji oleh keberadaan konstanta di
// password.go, bukan oleh test.
func TestMain(m *testing.M) {
	SetBcryptCostUntukTest(bcrypt.MinCost)
	os.Exit(m.Run())
}
