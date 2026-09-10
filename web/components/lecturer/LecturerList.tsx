"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter, usePathname, useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import type { LecturerListResponse } from "@/lib/api/types";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { DEPARTMENTS } from "@/components/lecturer/departments";

/**
 * Halaman daftar dosen — list + filter + pagination.
 *
 * Sumber kebenaran state filter adalah URL query string
 * (department, q, page). Back button / share link akan kembalikan
 * tampilan yang sama persis — keputusan M5c #3.
 *
 * q disimpan di URL setelah di-debounce (~350ms) supaya tidak spam
 * request ke backend ILIKE `%q%`.
 *
 * Query key convention: ['lecturers', { department, q, page }] — ikut
 * pola RINGKASAN-M5C keputusan #2 untuk konsistensi M5d nanti.
 */
export function LecturerList() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const department = searchParams.get("department") ?? "";
  const q = searchParams.get("q") ?? "";
  const pageRaw = searchParams.get("page") ?? "1";
  const page = Math.max(1, Number(pageRaw) || 1);

  // Input q lokal untuk UX mengetik yang responsif; sync ke URL via debounce.
  // Pakai pola "store & compare" yang disarankan React untuk menyinkronkan
  // state lokal dari perubahan eksternal (URL) — tanpa setState di dalam
  // useEffect, yang merupakan anti-pattern di React 19.
  const [qInput, setQInput] = useState(q);
  const [lastSyncedQ, setLastSyncedQ] = useState(q);
  if (q !== lastSyncedQ) {
    setLastSyncedQ(q);
    setQInput(q);
  }

  const qDebounced = useDebouncedValue(qInput, 350);

  // Debounced q → URL (reset halaman ke 1).
  useEffect(() => {
    if (qDebounced === q) return;
    const params = new URLSearchParams(searchParams.toString());
    if (qDebounced) params.set("q", qDebounced);
    else params.delete("q");
    params.delete("page");
    router.replace(`${pathname}?${params.toString()}`, { scroll: false });
    // deps sengaja hanya qDebounced — perubahan searchParams/router
    // bukan pemicu, melainkan efek dari sini.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qDebounced]);

  /** Tulis parameter URL dan trigger re-render. */
  const updateUrl = (changes: Record<string, string | null>) => {
    const params = new URLSearchParams(searchParams.toString());
    for (const [key, value] of Object.entries(changes)) {
      if (value === null || value === "") params.delete(key);
      else params.set(key, value);
    }
    router.replace(`${pathname}?${params.toString()}`, { scroll: false });
  };

  const handleDepartmentChange = (value: string) => {
    updateUrl({ department: value || null, page: null });
  };

  const handlePageChange = (newPage: number) => {
    if (newPage < 1) return;
    updateUrl({ page: newPage === 1 ? null : String(newPage) });
  };

  // Query — catatan: useApiFetch() aman dipakai untuk endpoint publik karena
  // AuthContext anonymous tidak kirim Authorization header.
  const fetchWithRetry = useApiFetch();
  const query = useQuery<LecturerListResponse>({
    queryKey: ["lecturers", { department, q, page }],
    queryFn: () => {
      const params = new URLSearchParams();
      if (department) params.set("department", department);
      if (q) params.set("q", q);
      if (page > 1) params.set("page", String(page));
      const qs = params.toString();
      return fetchWithRetry<LecturerListResponse>(
        `/lecturers${qs ? `?${qs}` : ""}`
      );
    },
    placeholderData: keepPreviousData,
  });

  return (
    <div className="container mx-auto max-w-4xl px-4 py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold">Cari Dosen</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Pilih dosen untuk melihat jadwal konsultasi yang tersedia.
        </p>
      </header>

      <Card className="mb-6">
        <CardHeader>
          <CardTitle className="text-base">Filter</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="filter-q">Nama</Label>
              <Input
                id="filter-q"
                type="text"
                value={qInput}
                onChange={(e) => setQInput(e.target.value)}
                placeholder="Cari berdasarkan nama…"
                disabled={query.isFetching}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="filter-department">Jurusan</Label>
              <select
                id="filter-department"
                value={department}
                onChange={(e) => handleDepartmentChange(e.target.value)}
                disabled={query.isFetching}
                className="flex h-9 w-full rounded-lg border border-input bg-background px-3 py-1 text-sm shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:border-ring disabled:cursor-not-allowed disabled:opacity-50"
              >
                <option value="">Semua jurusan</option>
                {DEPARTMENTS.map((d) => (
                  <option key={d} value={d}>
                    {d}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </CardContent>
      </Card>

      {query.isError ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-6 text-center text-sm text-destructive">
          Gagal memuat daftar dosen. Coba segarkan halaman.
        </div>
      ) : query.data && query.data.lecturers.length === 0 ? (
        <div className="rounded-lg border border-border px-4 py-12 text-center text-sm text-muted-foreground">
          Tidak ada dosen yang cocok dengan filter saat ini.
        </div>
      ) : (
        <>
          <div className="grid gap-3">
            {query.data?.lecturers.map((lecturer) => (
              <Link
                key={lecturer.id}
                href={`/dosen/${lecturer.id}`}
                className="block rounded-lg border border-border bg-card p-4 transition-colors hover:bg-muted/50"
              >
                <div className="flex flex-col gap-1">
                  <h2 className="font-medium">{lecturer.full_name}</h2>
                  <p className="text-sm text-muted-foreground">
                    {lecturer.department}
                    {lecturer.room ? ` · ${lecturer.room}` : ""}
                  </p>
                  {lecturer.bio && (
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {lecturer.bio}
                    </p>
                  )}
                </div>
              </Link>
            ))}
            {query.isLoading && (
              <>
                {[0, 1, 2].map((i) => (
                  <div
                    key={i}
                    className="h-20 animate-pulse rounded-lg border border-border bg-muted/30"
                  />
                ))}
              </>
            )}
          </div>

          {query.data && query.data.total > query.data.per_page && (
            <div className="mt-6 flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                Halaman {query.data.page} dari{" "}
                {Math.ceil(query.data.total / query.data.per_page)}
                {" "}({query.data.total} dosen)
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handlePageChange(page - 1)}
                  disabled={page <= 1 || query.isFetching}
                  type="button"
                >
                  Sebelumnya
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handlePageChange(page + 1)}
                  disabled={
                    page >=
                      Math.ceil(query.data.total / query.data.per_page) ||
                    query.isFetching
                  }
                  type="button"
                >
                  Berikutnya
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
