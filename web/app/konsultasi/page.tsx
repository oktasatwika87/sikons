"use client";

import { useState, useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { useCancelBooking } from "@/hooks/useCancelBooking";
import { useAuth } from "@/lib/auth/AuthContext";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { isBookingCancelable, bookingStatusLabel, bookingStatusClass } from "@/lib/booking";
import { formatJakartaLongDate, formatJakartaTime } from "@/lib/date";
import type { BookingListResponse, BookingResponse } from "@/lib/api/types";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertDialog } from "@/components/ui/alert-dialog";

type StatusFilter = "all" | "confirmed" | "cancelled" | "completed" | "no_show";

function KonsultasiContent() {
  const { auth } = useAuth();
  const fetchWithRetry = useApiFetch();

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const [cancelReason, setCancelReason] = useState("");

  const perPage = 20;

  const bookingsQuery = useQuery<BookingListResponse>({
    queryKey: ["bookings", statusFilter, 1, perPage],
    queryFn: () => {
      const params = new URLSearchParams();
      if (statusFilter !== "all") {
        params.set("status", statusFilter);
      }
      params.set("page", "1");
      params.set("per_page", perPage.toString());
      return fetchWithRetry<BookingListResponse>(`/bookings?${params.toString()}`);
    },
    placeholderData: keepPreviousData,
    enabled: auth.status === "authenticated",
  });

  const cancelMutation = useCancelBooking();

  const handleCancelClick = (bookingId: string) => {
    setCancelTarget(bookingId);
    setCancelReason("");
  };

  const handleCancelConfirm = () => {
    if (!cancelTarget) return;
    cancelMutation.mutate(
      { bookingId: cancelTarget, reason: cancelReason },
      {
        onSuccess: () => {
          setCancelTarget(null);
        },
      }
    );
  };

  const filters: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "Semua" },
    { value: "confirmed", label: "Akan Datang" },
    { value: "completed", label: "Selesai" },
    { value: "cancelled", label: "Dibatalkan" },
    { value: "no_show", label: "Tidak Hadir" },
  ];

  const isStudent = auth.user?.role === "student";

  return (
    <div className="container mx-auto max-w-4xl px-4 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Riwayat Konsultasi</h1>

      {/* Filter tabs */}
      <div className="mb-6 flex flex-wrap gap-2">
        {filters.map((f) => (
          <Button
            key={f.value}
            variant={statusFilter === f.value ? "default" : "outline"}
            size="sm"
            onClick={() => setStatusFilter(f.value)}
            type="button"
          >
            {f.label}
          </Button>
        ))}
      </div>

      {/* Loading state */}
      {bookingsQuery.isLoading && (
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div
              key={i}
              className="h-24 animate-pulse rounded-lg bg-muted"
            />
          ))}
        </div>
      )}

      {/* Error state */}
      {bookingsQuery.isError && (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-6 text-center text-sm text-destructive">
          Gagal memuat riwayat konsultasi.
        </div>
      )}

      {/* Empty state */}
      {bookingsQuery.data && bookingsQuery.data.bookings.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center">
            <p className="text-muted-foreground">
              Belum ada konsultasi.
            </p>
            {isStudent && (
              <Button
                variant="outline"
                className="mt-4"
                onClick={() => (window.location.href = "/dosen")}
                type="button"
              >
                Cari Dosen
              </Button>
            )}
          </CardContent>
        </Card>
      )}

      {/* Booking list — card layout untuk responsive */}
      {bookingsQuery.data && bookingsQuery.data.bookings.length > 0 && (
        <div className="space-y-4 md:hidden">
          {bookingsQuery.data.bookings.map((booking) => (
            <KonsultasiCard
              key={booking.id}
              booking={booking}
              isStudent={isStudent}
              onCancel={handleCancelClick}
            />
          ))}
        </div>
      )}

      {/* Booking list — table layout untuk desktop */}
      {bookingsQuery.data && bookingsQuery.data.bookings.length > 0 && (
        <div className="hidden md:block overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left">
                <th className="pb-3 pr-4 font-medium">Topik</th>
                <th className="pb-3 pr-4 font-medium">
                  {isStudent ? "Dosen" : "Mahasiswa"}
                </th>
                <th className="pb-3 pr-4 font-medium">Waktu</th>
                <th className="pb-3 pr-4 font-medium">Status</th>
                <th className="pb-3 font-medium">Aksi</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {bookingsQuery.data.bookings.map((booking) => (
                <KonsultasiRow
                  key={booking.id}
                  booking={booking}
                  isStudent={isStudent}
                  onCancel={handleCancelClick}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Cancel confirmation dialog */}
      <AlertDialog
        open={!!cancelTarget}
        onOpenChange={(open) => {
          if (!open) setCancelTarget(null);
        }}
        title="Batalkan Booking?"
        description="Apakah Anda yakin ingin membatalkan booking ini? Slot akan kembali tersedia untuk mahasiswa lain."
        cancelText="Tetap"
        confirmText="Batalkan"
        variant="destructive"
        loading={cancelMutation.isPending}
        onConfirm={handleCancelConfirm}
      />
    </div>
  );
}

// ------------------------------------------------------------------ KonsultasiCard (mobile)

interface KonsultasiCardProps {
  booking: BookingResponse;
  isStudent: boolean;
  onCancel: (id: string) => void;
}

function KonsultasiCard({ booking, isStudent, onCancel }: KonsultasiCardProps) {
  const slotStart = new Date(booking.slot.start_at);
  const canCancel =
    isStudent &&
    booking.status === "confirmed" &&
    isBookingCancelable(slotStart);

  const counterpartyName = isStudent
    ? booking.lecturer?.full_name
    : booking.student?.full_name;
  const counterpartySub = isStudent
    ? booking.lecturer?.department
    : (booking.student as { identity_number?: string })?.identity_number;

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <CardTitle className="text-base truncate">{booking.topic}</CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              {counterpartyName}
              {counterpartySub && (
                <span className="text-muted-foreground/70"> · {counterpartySub}</span>
              )}
            </p>
          </div>
          <span
            className={`shrink-0 rounded-full px-2.5 py-0.5 text-xs font-medium ${bookingStatusClass(booking.status)}`}
          >
            {bookingStatusLabel(booking.status)}
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          {formatJakartaLongDate(booking.slot.start_at)} ·{" "}
          {formatJakartaTime(booking.slot.start_at)}–
          {formatJakartaTime(booking.slot.end_at)}
        </p>
        {booking.description && (
          <p className="mt-2 text-sm text-muted-foreground line-clamp-2">
            {booking.description}
          </p>
        )}
        {/* Catatan dosen untuk baris completed — hanya untuk dosen */}
        {!isStudent && booking.lecturer_note && (
          <p className="mt-2 text-sm italic text-muted-foreground border-t pt-2">
            Catatan: {booking.lecturer_note}
          </p>
        )}
        {canCancel && (
          <div className="mt-3 flex justify-end">
            <Button
              variant="outline"
              size="sm"
              onClick={() => onCancel(booking.id)}
              type="button"
              className="text-destructive hover:text-destructive"
            >
              Batalkan
            </Button>
          </div>
        )}
        {!canCancel && isStudent && booking.status === "confirmed" && (
          <p className="mt-2 text-right text-xs text-muted-foreground">
            Tidak dapat dibatalkan (kurang dari 3 jam sebelum jadwal)
          </p>
        )}
      </CardContent>
    </Card>
  );
}

// ------------------------------------------------------------------ KonsultasiRow (desktop)

interface KonsultasiRowProps {
  booking: BookingResponse;
  isStudent: boolean;
  onCancel: (id: string) => void;
}

function KonsultasiRow({ booking, isStudent, onCancel }: KonsultasiRowProps) {
  const slotStart = new Date(booking.slot.start_at);
  const canCancel =
    isStudent &&
    booking.status === "confirmed" &&
    isBookingCancelable(slotStart);

  const counterpartyName = isStudent
    ? booking.lecturer?.full_name
    : booking.student?.full_name;
  const counterpartySub = isStudent
    ? booking.lecturer?.department
    : (booking.student as { identity_number?: string })?.identity_number;

  return (
    <tr>
      <td className="py-3 pr-4">
        <div className="font-medium">{booking.topic}</div>
        {booking.description && (
          <div className="text-xs text-muted-foreground line-clamp-1 max-w-xs">
            {booking.description}
          </div>
        )}
      </td>
      <td className="py-3 pr-4">
        <div>{counterpartyName}</div>
        {counterpartySub && (
          <div className="text-xs text-muted-foreground">{counterpartySub}</div>
        )}
      </td>
      <td className="py-3 pr-4 whitespace-nowrap">
        <div>{formatJakartaLongDate(booking.slot.start_at)}</div>
        <div className="text-xs text-muted-foreground">
          {formatJakartaTime(booking.slot.start_at)}–{formatJakartaTime(booking.slot.end_at)}
        </div>
      </td>
      <td className="py-3 pr-4">
        <span
          className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${bookingStatusClass(booking.status)}`}
        >
          {bookingStatusLabel(booking.status)}
        </span>
      </td>
      <td className="py-3">
        {canCancel && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => onCancel(booking.id)}
            type="button"
            className="text-destructive hover:text-destructive"
          >
            Batalkan
          </Button>
        )}
        {!canCancel && isStudent && booking.status === "confirmed" && (
          <span className="text-xs text-muted-foreground">–</span>
        )}
      </td>
    </tr>
  );
}

export default function KonsultasiPage() {
  return (
    <RequireAuth>
      <KonsultasiContent />
    </RequireAuth>
  );
}
