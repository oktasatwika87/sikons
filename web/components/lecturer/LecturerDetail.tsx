"use client";

import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import type {
  Lecturer,
  LecturerSlotsResponse,
} from "@/lib/api/types";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  formatJakartaShortDate,
  formatJakartaTime,
  formatWeekRangeLabel,
  groupSlotsByJakartaDate,
  isBeforeThisWeekJakarta,
  weekDayHeaders,
  weekRangeJakarta,
} from "@/lib/date";

/**
 * Halaman detail dosen — info + kalender slot mingguan read-only.
 *
 * Kalender:
 *  - Default minggu berjalan (Senin–Minggu Asia/Jakarta).
 *  - Tombol "Sebelumnya" dinonaktifkan kalau minggu sebelumnya seluruhnya
 *    sudah lewat (lihat isBeforeThisWeekJakarta).
 *  - Klik slot BELUM memicu apa-apa di M5c — booking form adalah M5d,
 *    jangan scope-creep ke situ (keputusan M5c #6).
 *
 * Format waktu SELALU lewat lib/date — tidak ada Date.toLocaleString
 * polos di sini.
 */
export function LecturerDetail({ lecturerId }: { lecturerId: string }) {
  const [weekAnchor, setWeekAnchor] = useState(() => new Date());
  const fetchWithRetry = useApiFetch();

  const lecturerQuery = useQuery<Lecturer>({
    queryKey: ["lecturer", lecturerId],
    queryFn: () => fetchWithRetry<Lecturer>(`/lecturers/${lecturerId}`),
  });

  // Hitung from/to dari weekAnchor lewat helper — konsisten dengan label
  // minggu dan header kolom kalender.
  const { from: weekFrom, to: weekTo } = weekRangeJakarta(weekAnchor);

  const slotsQuery = useQuery<LecturerSlotsResponse>({
    queryKey: [
      "lecturer",
      lecturerId,
      "slots",
      { from: weekFrom.toISOString(), to: weekTo.toISOString() },
    ],
    queryFn: () => {
      const params = new URLSearchParams();
      params.set("from", weekFrom.toISOString());
      params.set("to", weekTo.toISOString());
      return fetchWithRetry<LecturerSlotsResponse>(
        `/lecturers/${lecturerId}/slots?${params.toString()}`
      );
    },
    placeholderData: keepPreviousData,
    enabled: !!lecturerQuery.data,
  });

  const handlePrevWeek = () => {
    if (isBeforeThisWeekJakarta(weekAnchor)) return;
    const next = new Date(weekAnchor);
    next.setDate(next.getDate() - 7);
    setWeekAnchor(next);
  };

  const handleNextWeek = () => {
    const next = new Date(weekAnchor);
    next.setDate(next.getDate() + 7);
    setWeekAnchor(next);
  };

  const handleThisWeek = () => setWeekAnchor(new Date());

  if (lecturerQuery.isError) {
    return (
      <div className="container mx-auto max-w-4xl px-4 py-8">
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-6 text-center text-sm text-destructive">
          {lecturerQuery.error instanceof Error
            ? lecturerQuery.error.message
            : "Gagal memuat data dosen."}
        </div>
      </div>
    );
  }

  const lecturer = lecturerQuery.data;
  const headers = weekDayHeaders(weekAnchor);
  const slotsByDate = slotsQuery.data
    ? groupSlotsByJakartaDate(slotsQuery.data.slots)
    : new Map<string, LecturerSlotsResponse["slots"]>();
  const prevDisabled =
    isBeforeThisWeekJakarta(weekAnchor) || slotsQuery.isFetching;

  return (
    <div className="container mx-auto max-w-5xl px-4 py-8">
      <header className="mb-6">
        {lecturer ? (
          <>
            <h1 className="text-2xl font-semibold">{lecturer.full_name}</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {lecturer.department}
              {lecturer.room ? ` · ${lecturer.room}` : ""}
            </p>
            {lecturer.bio && (
              <p className="mt-3 text-sm">{lecturer.bio}</p>
            )}
          </>
        ) : (
          <div className="space-y-2">
            <div className="h-7 w-48 animate-pulse rounded bg-muted" />
            <div className="h-4 w-64 animate-pulse rounded bg-muted" />
          </div>
        )}
      </header>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle className="text-base">
              {formatWeekRangeLabel(weekAnchor)}
            </CardTitle>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handlePrevWeek}
                disabled={prevDisabled}
                type="button"
              >
                Sebelumnya
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleThisWeek}
                disabled={slotsQuery.isFetching}
                type="button"
              >
                Minggu ini
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleNextWeek}
                disabled={slotsQuery.isFetching}
                type="button"
              >
                Berikutnya
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {slotsQuery.isError ? (
            <p className="py-6 text-center text-sm text-destructive">
              Gagal memuat jadwal dosen.
            </p>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-7">
              {headers.map((day) => {
                const slots = slotsByDate.get(day.key) ?? [];
                return (
                  <div
                    key={day.key}
                    className="rounded-lg border border-border bg-background"
                  >
                    <div className="border-b border-border px-3 py-2">
                      <p className="text-xs font-medium text-muted-foreground">
                        {day.label}
                      </p>
                      <p className="text-sm font-medium">
                        {formatJakartaShortDate(day.date)}
                      </p>
                    </div>
                    <div className="flex flex-col gap-1 p-2">
                      {slots.length === 0 ? (
                        <p className="px-1 py-3 text-center text-xs text-muted-foreground">
                          {slotsQuery.isLoading ? "Memuat…" : "Tidak ada slot"}
                        </p>
                      ) : (
                        slots.map((slot) => {
                          const isBooked = slot.status === "booked";
                          return (
                            <button
                              key={slot.id}
                              type="button"
                              disabled={isBooked}
                              title={
                                isBooked
                                  ? "Slot sudah dipesan"
                                  : "Slot tersedia (M5d akan menambahkan form booking)"
                              }
                              className={
                                "rounded-md border px-2 py-1.5 text-left text-xs transition-colors " +
                                (isBooked
                                  ? "cursor-not-allowed border-border bg-muted text-muted-foreground line-through"
                                  : "border-border bg-card hover:border-primary/40 hover:bg-primary/5")
                              }
                            >
                              {formatJakartaTime(slot.start_at)}–
                              {formatJakartaTime(slot.end_at)}
                            </button>
                          );
                        })
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          <p className="mt-4 text-xs text-muted-foreground">
            Klik slot untuk memesan — fitur ini akan tersedia setelah
            modul M5d.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
