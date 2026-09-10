# RINGKASAN — M5b: Auth Frontend

## Status Terbaru (revisi 2026-09-10)

- `hooks/__tests__/useApiFetch.test.tsx` — **4 test lulus genuine** (sebelumnya placeholder/xfail).
- `components/auth/__tests__/LoginForm.test.tsx` — **3 test baru** untuk redirect `?next=`.
- Total: **21 test lulus** (5 test file).
- `lib/auth/AuthContext.tsx` — `refreshTokens` sekarang mengembalikan `{ ok: boolean }` agar
  useApiFetch tidak membaca closure `auth` yang stale.
- `hooks/useApiFetch.ts` — authRef untuk token terbaru, status check pakai `result.ok`
  (bukan `auth.status` dari closure).
- Bug closure di useApiFetch yang sebelumnya menyebabkan retry endpoint sia-sia setelah
  refresh gagal sudah diperbaiki lewat kombinasi dua perubahan di atas.

## Test useApiFetch (pendekatan baru)

**Yang dibuang:** `vi.mock("@/lib/auth/AuthContext")` + `vi.hoisted(...)` — mocking di
layer yang salah. `vi.hoisted` jalan saat module init, sebelum `let` diinisialisasi
→ TDZ. State tidak bisa di-pass ke mock factory.

**Yang dipakai:** MSW + closure biasa untuk counter. Hitungan panggilan
`/auth/refresh` dan endpoint disimpan di object `state` di dalam `it()`, diakses
langsung oleh handler MSW:

```ts
const state = { refreshCalls: 0, endpointCalls: 0 };
server.use(
  http.post(`${BASE_URL}/auth/refresh`, () => {
    state.refreshCalls++;
    return HttpResponse.json({ ... });
  }),
  http.get(`${BASE_URL}/protected`, () => {
    state.endpointCalls++;
    if (state.endpointCalls === 1) return HttpResponse.json(..., { status: 401 });
    return HttpResponse.json({ data: "success" });
  })
);
```

### Empat test yang ditulis

| # | Test | Yang diverifikasi |
|---|------|-------------------|
| 1 | `retry-once sukses` | 401 TOKEN_EXPIRED → refresh → retry → 200. `refreshCalls === 1`, `endpointCalls === 2`. |
| 2 | `single-flight` | 3 `useApiFetch` paralel, semua dapat 401. `refreshCalls === 1` (bukan 3), `endpointCalls === 6` (3 × 401 + 3 × 200). |
| 3 | `409 REFRESH_IN_PROGRESS` | refresh pertama 409 → tunggu 500ms → refresh kedua sukses → retry endpoint → 200. `refreshCalls === 2`, `elapsed >= 450ms`. |
| 4 | `refresh gagal final` | refresh 401 REFRESH_TOKEN_INVALID → `result.ok === false` → throw `UNAUTHENTICATED`. `endpointCalls === 1` (tidak ada retry sia-sia). Status AuthContext berakhir `anonymous`. |

### Komponen pembantu di test

- `ReadyChildren` — render `<FetchProbe>` hanya setelah `auth.status !== "loading"`.
  Tanpa ini, balapan antara bootstrap `/me` dan fetch pertama bisa membuat closure
  `auth.status` masih "loading" di probe pertama.
- `FetchProbe` — komponen kecil yang memanggil `useApiFetch()` lalu `onResult`/`onError`.
  Pakai `useRef` untuk callback supaya identitas referensinya stabil.
- `StatusRecorder` — rekam setiap perubahan `auth.status` lewat `useEffect`
  (bukan saat render, karena React refs tidak boleh diakses saat render).

## Test LoginForm (baru)

LoginForm sudah membaca `?next=` sejak M5b awal (`searchParams.get("next")` lalu
`router.push(next ? decodeURIComponent(next) : "/")`), tapi belum ada test-nya.
Test ini ditulis dengan `vi.mock("next/navigation")` — diizinkan karena next/router
bukan layer HTTP; beda dengan useApiFetch yang murni MSW.

| # | Test | Yang diverifikasi |
|---|------|-------------------|
| 1 | `tanpa ?next=` | `router.push("/")` dipanggil setelah login sukses. |
| 2 | `dengan ?next=/akun` | `router.push("/akun")` dipanggil, `router.push("/")` TIDAK dipanggil. |
| 3 | `dengan ?next= kosong` | `searchParams.get("next")` kembalikan `""` → fallback ke `/`. |

## Bug yang Ditemukan Saat Menulis Test

### Closure staleness di useApiFetch

`fetchWithRetry` di-memoize via `useCallback` dengan deps `[auth.accessToken,
auth.status, refreshTokens]`. Saat refresh gagal dan `AuthContext` memanggil
`setAnonymous()`, closure `auth` yang ada di `fetchWithRetry` masih snapshot
lama — `auth.status === "authenticated"`. Akibatnya `if (auth.status !== "authenticated")`
selalu FALSE → useApiFetch melakukan retry endpoint sekali lagi dengan
header `Authorization: Bearer <stale-or-null>` → server kembalikan 401 → throw
`UNAUTHENTICATED`. End behavior benar, tapi ada 1 fetch sia-sia di tengah.

**Fix #1:** `refreshTokens` sekarang mengembalikan `{ ok: boolean }` langsung
dari hasil eksekusinya, bukan `void`. Pemanggil (useApiFetch) tidak perlu
membaca `auth.status` lagi:

```ts
const result = await promise;
if (!result.ok) throw new ApiError("UNAUTHENTICATED", ...);
```

**Fix #2:** Untuk `doFetch` (yang mengirim Authorization header), dipakai
`authRef` yang di-update via `useEffect([auth])`. Trade-off: kalau React belum
sempat commit re-render + useEffect sebelum retry `doFetch` dipanggil, ref
masih stale. Untuk skenario ini, server akan kembalikan 401 dan throw kedua
menangkapnya. Untuk sekarang trade-off ini diterima — alternatifnya adalah
pass callback dari `setAuth` ke `refreshTokens`, yang menambah coupling.

**Kenapa dua fix?** #1 menutup jalur "refresh gagal" tanpa baca closure.
#2 menutup jalur "doFetch pakai token stale" saat refresh sukses. Keduanya
sumber bug yang sama (closure) tapi di titik yang berbeda.

## Verifikasi

- `npm run lint` — 0 error (1 warning pre-existing untuk `mockServiceWorker.js`,
  di luar scope perubahan ini).
- `npm run build` — sukses, TypeScript bersih.
- `npm test` — **21 test lulus** (sebelumnya 15, sekarang +6).
- Cek manual smart quote/CJK/box-drawing di file baru — bersih.

## Berkas yang Berubah/Ditambah

| File | Perubahan |
|------|-----------|
| `hooks/useApiFetch.ts` | Tambah `useEffect` import; `authRef` + `result.ok`-based check; deps `useCallback` jadi `[refreshTokens]` saja. |
| `lib/auth/AuthContext.tsx` | `refreshTokens` return type `{ ok: boolean }`; nested try di 409 path supaya return type jelas. `useEffect` proaktif pakai `.then((r) => { if (!r.ok) setAnonymous() })`. |
| `hooks/__tests__/useApiFetch.test.tsx` | **Baru** — 4 test (ganti placeholder `.ts` dengan implementasi `.tsx` sebenarnya). |
| `hooks/__tests__/useApiFetch.test.ts` | **Dihapus** — placeholder. |
| `components/auth/__tests__/LoginForm.test.tsx` | **Baru** — 3 test untuk `?next=` redirect. |
