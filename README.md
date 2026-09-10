# SIKONS — Sistem Reservasi Konsultasi Dosen

> README ini masih kerangka. Diisi lengkap di M8 — berisi KEPUTUSAN dan
> TRADE-OFF, bukan cara install.

## Menjalankan di lokal

```bash
cp .env.example .env
make up            # Postgres
make migrate-up    # bikin tabel
make run           # API di http://localhost:8080
```

Cek:

```bash
curl localhost:8080/healthz   # {"status":"ok"}
curl localhost:8080/readyz    # {"status":"ready"}
```

Test:

```bash
make test-db     # sekali saja: bikin database uji + migrasinya
make test        # semua test, termasuk 100 request paralel ke slot yang sama
```

`make help` menampilkan semua perintah.

## Frontend

```bash
cd web && npm install && npm run dev
```

Frontend Next.js 15 (App Router) di port 3000. API backend di 8080 sudah dikonfigurasi via `NEXT_PUBLIC_API_URL`.

## Status

- [x] M1 — kerangka repo, Docker Compose, skema database
- [ ] M2 — auth + RBAC
- [~] M3 — booking + locking + concurrency test selesai; ketersediaan & generator slot menyusul
- [ ] M4 — cancel, worker reminder
- [ ] M5–M6 — frontend
- [ ] M7 — CI + deploy
- [ ] M8 — dokumentasi
