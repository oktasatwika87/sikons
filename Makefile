# Perintah-perintah yang sering dipakai. Ditulis di Makefile supaya tidak ada
# perintah panjang yang cuma hidup di riwayat terminal dan hilang saat ganti
# laptop — dan supaya CI nanti menjalankan perintah yang PERSIS SAMA dengan
# yang kamu jalankan di lokal.

# Tanda minus di depan: kalau .env belum ada, make tetap jalan (dan target
# `help` tetap bisa dipakai) alih-alih mati dengan error yang membingungkan.
-include .env
export

.PHONY: help up down reset logs psql migrate-up migrate-down migrate-version migrate-new test-db run test test-unit test-concurrency test-cover tidy fmt vet

help: ## Tampilkan daftar perintah
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

up: ## Nyalakan Postgres + Redis
	docker compose up -d
	@echo "menunggu database siap..."
	@until docker compose exec -T postgres pg_isready -U sikons -d sikons >/dev/null 2>&1; do sleep 1; done
	@echo "siap."

down: ## Matikan container (data tetap aman di volume)
	docker compose down

reset: ## Matikan DAN hapus datanya (mulai dari nol)
	docker compose down -v

logs: ## Lihat log container
	docker compose logs -f

psql: ## Masuk ke psql
	docker compose exec postgres psql -U sikons -d sikons

# golang-migrate dijalankan sebagai service compose (lihat docker-compose.yml),
# supaya kamu tidak perlu memasang CLI tambahan DAN supaya container-nya bisa
# memanggil Postgres lewat jaringan internal compose — bukan lewat localhost,
# yang di macOS menunjuk ke VM Docker, bukan ke laptopmu.
migrate-up: ## Jalankan semua migrasi
	docker compose run --rm migrate up

migrate-down: ## Mundur satu migrasi
	docker compose run --rm migrate down 1

migrate-version: ## Lihat versi migrasi yang sedang aktif
	docker compose run --rm migrate version

migrate-new: ## Buat file migrasi baru: make migrate-new name=tambah_sesuatu
	docker compose run --rm --entrypoint migrate migrate \
		create -ext sql -dir /migrations -seq $(name)

run: ## Jalankan API server
	go run ./cmd/api

seed: ## Isi database dengan data demo (akun admin, dosen, mahasiswa)
	go run ./cmd/seed

# Database uji TERPISAH dari database development. Test membersihkan tabel
# dengan TRUNCATE di awal tiap test — kalau diarahkan ke database dev, data
# percobaanmu ikut hilang setiap kali test jalan.
test-db: ## Siapkan database uji (sekali saja, atau setelah `make reset`)
	-docker compose exec -T postgres psql -U sikons -d postgres -c "CREATE DATABASE sikons_test"
	docker compose run --rm --entrypoint migrate migrate \
		-path=/migrations \
		-database="postgres://sikons:sikons_dev@postgres:5432/sikons_test?sslmode=disable" up

test: ## Semua test (integrasi ikut jalan kalau TEST_DATABASE_URL diisi)
	go test ./... -race -count=1

test-unit: ## Test unit saja — cepat, tidak butuh Docker
	TEST_DATABASE_URL= go test ./... -race -count=1

test-concurrency: ## Hanya test perebutan slot, dengan output verbose
	go test ./internal/booking -race -count=1 -v -run 'Paralel|Sepuluh'

test-cover: ## Test + laporan coverage di browser
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "buka coverage.html"

tidy: ## Rapikan go.mod
	go mod tidy

fmt: ## Format seluruh kode
	go fmt ./...

vet: ## Analisis statis bawaan Go
	go vet ./...
