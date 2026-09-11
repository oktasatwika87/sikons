"use client";

import { useState } from "react";
import { useSearchParams } from "next/navigation";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { useCancelBooking } from "@/hooks/useCancelBooking";
import { useAuth } from "@/lib/auth/AuthContext";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { isBookingCancelable, bookingStatusLabel, bookingStatusClass } from "@/lib/booking";
import { formatJakartaLongDate, formatJakartaTime } from "@/lib/date";
import type { BookingListResponse } from "@/lib/api/types";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertDialog } from "@/components/ui/alert-dialog";

type StatusFilter = "all" | "confirmed" | "cancelled" | "completed" | "no_show";

function BookingSayaContent() {
  const { auth } = useAuth();
  const fetchWithRetry = useApiFetch();
  const searchParams = useSearchParams();

  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const [cancelReason, setCancelReason] = useState("");

  // Parse page dari URL query param.
  const page = parseInt(searchParams.get("page") ?? "1", 10);
  const perPage = 10;

  const bookingsQuery = useQuery<BookingListResponse>({
    queryKey: ["bookings", statusFilter, page, perPage],
    queryFn: () => {
      const params = new URLSearchParams();
      if (statusFilter !== "all") {
        params.set("status", statusFilter);
      }
      params.set("page", page.toString());
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
        onError: () => {
          // Error sudah di-handle di hook.
        },
      }
    );
  };

  const totalPages = bookingsQuery.data
    ? Math.ceil(bookingsQuery.data.total / perPage)
    : 0;

  const filters: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "Semua" },
    { value: "confirmed", label: "Akan Datang" },
    { value: "completed", label: "Selesai" },
    { value: "cancelled", label: "Dibatalkan" },
    { value: "no_show", label: "Tidak Hadir" },
  ];

  // Parsing slot_start_at dari RFC3339 ke Date.
  const parseSlotStart = (rfc3339: string) => new Date(rfc3339);

  return (
    <div className="container mx-auto max-w-4xl px-4 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Booking Saya</h1>

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
          Gagal memuat daftar booking.
        </div>
      )}

      {/* Empty state */}
      {bookingsQuery.data && bookingsQuery.data.bookings.length === 0 && (
        <Card>
          <CardContent className="py-12 text-center">
            <p className="text-muted-foreground">
              Belum ada booking.
            </p>
            <Button
              variant="outline"
              className="mt-4"
              onClick={() => (window.location.href = "/dosen")}
              type="button"
            >
              Cari Dosen
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Booking list */}
      {bookingsQuery.data && bookingsQuery.data.bookings.length > 0 && (
        <div className="space-y-4">
          {bookingsQuery.data.bookings.map((booking) => {
            const slotStart = parseSlotStart(booking.slot.start_at);
            const canCancel =
              auth.user?.role === "student" &&
              booking.status === "confirmed" &&
              isBookingCancelable(slotStart);

            return (
              <Card key={booking.id}>
                <CardHeader className="pb-2">
                  <div className="flex items-start justify-between gap-4">
                    <div>
                      <CardTitle className="text-base">
                        {booking.topic}
                      </CardTitle>
                      <p className="mt-1 text-sm text-muted-foreground">
                        {formatJakartaLongDate(booking.slot.start_at)} ·{" "}
                        {formatJakartaTime(booking.slot.start_at)}–
                        {formatJakartaTime(booking.slot.end_at)}
                      </p>
                      {booking.lecturer && (
                        <p className="mt-1 text-sm text-muted-foreground">
                          dengan {booking.lecturer.full_name}
                        </p>
                      )}
                    </div>
                    <span
                      className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${bookingStatusClass(booking.status)}`}
                    >
                      {bookingStatusLabel(booking.status)}
                    </span>
                  </div>
                </CardHeader>
                <CardContent>
                  {booking.description && (
                    <p className="mb-4 text-sm text-muted-foreground">
                      {booking.description}
                    </p>
                  )}
                  {canCancel && (
                    <div className="flex justify-end">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleCancelClick(booking.id)}
                        type="button"
                        className="text-destructive hover:text-destructive"
                      >
                        Batalkan
                      </Button>
                    </div>
                  )}
                  {/* Client-side disable hint (bukan penegakan) */}
                  {auth.user?.role === "student" &&
                    booking.status === "confirmed" &&
                    !isBookingCancelable(slotStart) && (
                      <p className="text-right text-xs text-muted-foreground">
                        Tidak dapat dibatalkan (kurang dari 3 jam sebelum jadwal)
                      </p>
                    )}
                </CardContent>
              </Card>
            );
          })}

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                Halaman {page} dari {totalPages}
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    const params = new URLSearchParams(searchParams.toString());
                    params.set("page", String(page - 1));
                    window.location.href = `/booking-saya?${params.toString()}`;
                  }}
                  disabled={page <= 1}
                  type="button"
                >
                  Sebelumnya
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    const params = new URLSearchParams(searchParams.toString());
                    params.set("page", String(page + 1));
                    window.location.href = `/booking-saya?${params.toString()}`;
                  }}
                  disabled={page >= totalPages}
                  type="button"
                >
                  Berikutnya
                </Button>
              </div>
            </div>
          )}
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

export default function BookingSayaPage() {
  return (
    <RequireAuth>
      <BookingSayaContent />
    </RequireAuth>
  );
}
