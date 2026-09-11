"use client";

import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { useCompleteBooking } from "@/hooks/useCompleteBooking";
import { useNoShowBooking } from "@/hooks/useNoShowBooking";
import { RequireAuth } from "@/components/auth/RequireAuth";
import {
  groupDashboardBookings,
  isSessionActionable,
} from "@/lib/booking";
import { formatJakartaLongDate, formatJakartaTime } from "@/lib/date";
import type { BookingListResponse, BookingResponse } from "@/lib/api/types";
import { z } from "zod";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AlertDialog } from "@/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";

// Zod schema untuk catatan dosen (konsisten dengan backend: maks 2000 karakter)
const LecturerNoteSchema = z.string().max(2000, "Catatan maksimal 2000 karakter").optional();

function DashboardContent() {
  const fetchWithRetry = useApiFetch();

  // State untuk dialog
  const [completeTarget, setCompleteTarget] = useState<BookingResponse | null>(null);
  const [completeNote, setCompleteNote] = useState("");
  const [noteError, setNoteError] = useState<string | null>(null);
  const [noShowTarget, setNoShowTarget] = useState<BookingResponse | null>(null);

  // Fetch booking confirmed (sekali, tanpa pagination lanjutan)
  const bookingsQuery = useQuery<BookingListResponse>({
    queryKey: ["bookings", "confirmed", 1, 100],
    queryFn: () => {
      const params = new URLSearchParams({
        status: "confirmed",
        page: "1",
        per_page: "100",
      });
      return fetchWithRetry<BookingListResponse>(`/bookings?${params.toString()}`);
    },
    enabled: true,
  });

  // Mutations
  const completeMutation = useCompleteBooking();
  const noShowMutation = useNoShowBooking();

  // Kelompokkan booking ke tiga bucket
  const buckets = useMemo(() => {
    if (!bookingsQuery.data) {
      return { needsFollowUp: [], today: [], upcoming: new Map() };
    }
    // Sort ascending berdasarkan start_at sebelum kelompokkan
    const sorted = [...bookingsQuery.data.bookings].sort(
      (a, b) => new Date(a.slot.start_at).getTime() - new Date(b.slot.start_at).getTime()
    );
    return groupDashboardBookings(sorted, new Date());
  }, [bookingsQuery.data]);

  // Handlers untuk complete
  const handleCompleteClick = (booking: BookingResponse) => {
    setCompleteTarget(booking);
    setCompleteNote(booking.lecturer_note ?? "");
    setNoteError(null);
  };

  const handleCompleteConfirm = () => {
    if (!completeTarget) return;

    // Validasi catatan
    const validation = LecturerNoteSchema.safeParse(completeNote);
    if (!validation.success) {
      setNoteError(validation.error.issues[0].message);
      return;
    }

    completeMutation.mutate(
      { bookingId: completeTarget.id, lecturerNote: completeNote || undefined },
      {
        onSuccess: () => {
          setCompleteTarget(null);
          setCompleteNote("");
          setNoteError(null);
        },
      }
    );
  };

  // Handlers untuk no-show
  const handleNoShowClick = (booking: BookingResponse) => {
    setNoShowTarget(booking);
  };

  const handleNoShowConfirm = () => {
    if (!noShowTarget) return;
    noShowMutation.mutate(
      { bookingId: noShowTarget.id },
      {
        onSuccess: () => {
          setNoShowTarget(null);
        },
      }
    );
  };

  // Loading state
  if (bookingsQuery.isLoading) {
    return (
      <div className="container mx-auto max-w-4xl px-4 py-8">
        <h1 className="mb-6 text-2xl font-semibold">Dashboard</h1>
        <div className="space-y-4">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded-lg bg-muted" />
          ))}
        </div>
      </div>
    );
  }

  // Error state
  if (bookingsQuery.isError) {
    return (
      <div className="container mx-auto max-w-4xl px-4 py-8">
        <h1 className="mb-6 text-2xl font-semibold">Dashboard</h1>
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-6 text-center text-sm text-destructive">
          Gagal memuat daftar booking.
        </div>
      </div>
    );
  }

  const { needsFollowUp, today, upcoming } = buckets;
  const hasNoBookings = needsFollowUp.length === 0 && today.length === 0 && upcoming.size === 0;

  return (
    <div className="container mx-auto max-w-4xl px-4 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Dashboard</h1>

      {/* Empty state */}
      {hasNoBookings && (
        <Card>
          <CardContent className="py-12 text-center">
            <p className="text-muted-foreground">
              Belum ada booking yang perlu ditindaklanjuti.
            </p>
          </CardContent>
        </Card>
      )}

      {/* Needs Follow Up */}
      {needsFollowUp.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-lg font-medium text-destructive">
            Perlu Ditindaklanjuti ({needsFollowUp.length})
          </h2>
          <div className="space-y-3">
            {needsFollowUp.map((booking) => (
              <BookingCard
                key={booking.id}
                booking={booking}
                onComplete={handleCompleteClick}
                onNoShow={handleNoShowClick}
              />
            ))}
          </div>
        </section>
      )}

      {/* Hari Ini */}
      {today.length > 0 && (
        <section className="mb-8">
          <h2 className="mb-3 text-lg font-medium">
            Hari Ini ({today.length} sesi)
          </h2>
          <div className="space-y-3">
            {today.map((booking) => (
              <BookingCard
                key={booking.id}
                booking={booking}
                onComplete={handleCompleteClick}
                onNoShow={handleNoShowClick}
              />
            ))}
          </div>
        </section>
      )}

      {/* Mendatang */}
      {upcoming.size > 0 && (
        <section>
          <h2 className="mb-3 text-lg font-medium text-muted-foreground">
            Mendatang
          </h2>
          <div className="space-y-6">
            {[...upcoming.entries()].map(([dateKey, bookings]: [string, BookingResponse[]]) => (
              <div key={dateKey}>
                <h3 className="mb-2 text-sm font-medium text-muted-foreground">
                  {formatJakartaLongDate(dateKey)}
                </h3>
                <div className="space-y-3">
                  {bookings.map((booking) => (
                    <BookingCard
                      key={booking.id}
                      booking={booking}
                      onComplete={handleCompleteClick}
                      onNoShow={handleNoShowClick}
                      upcoming
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Dialog Tandakan Selesai */}
      <CompleteDialog
        open={!!completeTarget}
        onOpenChange={(open) => {
          if (!open) {
            setCompleteTarget(null);
            setNoteError(null);
          }
        }}
        booking={completeTarget}
        note={completeNote}
        onNoteChange={setCompleteNote}
        noteError={noteError}
        loading={completeMutation.isPending}
        error={completeMutation.error}
        onConfirm={handleCompleteConfirm}
      />

      {/* Dialog Konfirmasi Tidak Hadir */}
      <AlertDialog
        open={!!noShowTarget}
        onOpenChange={(open) => {
          if (!open) setNoShowTarget(null);
        }}
        title="Tandai Tidak Hadir?"
        description={
          noShowTarget
            ? `Yakin tandai "${noShowTarget.topic}" dengan ${noShowTarget.student?.full_name ?? "mahasiswa"} sebagai tidak hadir?`
            : undefined
        }
        cancelText="Batal"
        confirmText="Ya, Tidak Hadir"
        variant="destructive"
        loading={noShowMutation.isPending}
        onConfirm={handleNoShowConfirm}
      />
    </div>
  );
}

// ------------------------------------------------------------------ BookingCard

interface BookingCardProps {
  booking: BookingResponse;
  onComplete: (booking: BookingResponse) => void;
  onNoShow: (booking: BookingResponse) => void;
  upcoming?: boolean;
}

function BookingCard({ booking, onComplete, onNoShow, upcoming = false }: BookingCardProps) {
  const slotStart = new Date(booking.slot.start_at);
  const actionable = isSessionActionable(slotStart);

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-start justify-between gap-4">
          <div>
            <CardTitle className="text-base">{booking.topic}</CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              {formatJakartaLongDate(booking.slot.start_at)} ·{" "}
              {formatJakartaTime(booking.slot.start_at)}–
              {formatJakartaTime(booking.slot.end_at)}
            </p>
            {booking.student && (
              <p className="mt-1 text-sm text-muted-foreground">
                dengan {booking.student.full_name}
              </p>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {booking.description && (
          <p className="mb-4 text-sm text-muted-foreground">
            {booking.description}
          </p>
        )}
        {actionable && !upcoming && (
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => onNoShow(booking)}
              type="button"
            >
              Tidak Hadir
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={() => onComplete(booking)}
              type="button"
            >
              Tandai Selesai
            </Button>
          </div>
        )}
        {!actionable && !upcoming && (
          <p className="text-right text-xs text-muted-foreground">
            Menunggu waktu sesi dimulai
          </p>
        )}
      </CardContent>
    </Card>
  );
}

// ------------------------------------------------------------------ CompleteDialog

interface CompleteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  booking: BookingResponse | null;
  note: string;
  onNoteChange: (note: string) => void;
  noteError: string | null;
  loading: boolean;
  error: { type: string; message: string } | null;
  onConfirm: () => void;
}

function CompleteDialog({
  open,
  onOpenChange,
  booking,
  note,
  onNoteChange,
  noteError,
  loading,
  error,
  onConfirm,
}: CompleteDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Tandai Selesai</DialogTitle>
          <DialogDescription>
            {booking && `Sesi "${booking.topic}" dengan ${booking.student?.full_name ?? "mahasiswa"}`}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <label htmlFor="lecturer-note" className="text-sm font-medium">
            Catatan (opsional)
          </label>
          <textarea
            id="lecturer-note"
            className="min-h-[120px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
            placeholder="Catatan tentang sesi ini..."
            value={note}
            onChange={(e) => onNoteChange(e.target.value)}
            maxLength={2000}
            disabled={loading}
          />
          <div className="flex items-center justify-between">
            {noteError ? (
              <p className="text-xs text-destructive">{noteError}</p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {note.length}/2000 karakter
              </p>
            )}
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          Catatan <strong>tidak bisa diubah</strong> setelah disimpan.
        </p>

        {error && (
          <p className="text-sm text-destructive">
            {error.message}
          </p>
        )}

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
            type="button"
          >
            Batal
          </Button>
          <Button
            variant="default"
            onClick={onConfirm}
            disabled={loading}
            type="button"
          >
            {loading ? "Menyimpan..." : "Simpan"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export default function DashboardPage() {
  return (
    <RequireAuth>
      <DashboardContent />
    </RequireAuth>
  );
}
