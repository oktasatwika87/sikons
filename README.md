# SIKONS — Sistem Reservasi Konsultasi Dosen

> README ini masih kerangka. Diisi lengkap di M8 — berisi KEPUTUSAN dan
> TRADE-OFF, bukan cara install.

## Menjalankan di lokal

```bash
cp .env.example .env
make up            # Postgres + Redis
make migrate-up    # bikin tabel
make run           # API di http://localhost:8080
```

Cek:

```bash
curl localhost:8080/healthz   # {"status":"ok"}
curl localhost:8080/readyz    # {"status":"ready"}
```

`make help` menampilkan semua perintah.

## Status

- [x] M1 — kerangka repo, Docker Compose, skema database
- [ ] M2 — auth + RBAC
- [ ] M3 — ketersediaan, generator slot, booking + concurrency test
- [ ] M4 — cancel, worker reminder
- [ ] M5–M6 — frontend
- [ ] M7 — CI + deploy
- [ ] M8 — dokumentasi
