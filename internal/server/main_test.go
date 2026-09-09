package server

import (
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/oktasatwika/sikons/internal/auth"
)

// Sama alasannya dengan TestMain di package auth: test integrasi di package ini
// mendaftarkan banyak user, dan tiap register memanggil bcrypt.
func TestMain(m *testing.M) {
	auth.SetBcryptCostUntukTest(bcrypt.MinCost)
	os.Exit(m.Run())
}
