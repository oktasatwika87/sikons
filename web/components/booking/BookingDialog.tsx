"use client";

import { useState } from "react";
import Link from "next/link";
import { useCreateBooking } from "@/hooks/useCreateBooking";
import type { Slot } from "@/lib/api/types";
import { formatJakartaLongDate, formatJakartaTime } from "@/lib/date";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogClose,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface BookingDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  slot: Slot | null;
  lecturerId: string;
  lecturerName: string;
  /** Callback saat error SLOT_ALREADY_BOOKED — parent component mengatur banner. */
  onAlreadyBookedError: (message: string) => void;
}

export function BookingDialog({
  open,
  onOpenChange,
  slot,
  lecturerId,
  lecturerName,
  onAlreadyBookedError,
}: BookingDialogProps) {
  const [topic, setTopic] = useState("");
  const [description, setDescription] = useState("");

  const { mutate, isPending, error, resetError } = useCreateBooking(lecturerId);

  // Reset form saat dialog ditutup.
  const handleOpenChange = (newOpen: boolean) => {
    if (!newOpen) {
      setTopic("");
      setDescription("");
      resetError();
    }
    onOpenChange(newOpen);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!slot) return;

    mutate(
      {
        slotId: slot.id,
        topic: topic.trim(),
        description: description.trim(),
      },
      {
        onSuccess: () => {
          setTopic("");
          setDescription("");
          onOpenChange(false);
        },
        onError: (err) => {
          // SLOT_ALREADY_BOOKED: panggil callback, lalu tutup dialog.
          // Banner akan ditampilkan oleh parent component.
          if (err.type === "already_booked") {
            onAlreadyBookedError(err.message);
            onOpenChange(false);
          }
          // Error lain tetap ditampilkan di dalam dialog.
        },
      }
    );
  };

  // Tentukan error message yang user-friendly.
  // Fallback "Terjadi kesalahan" untuk kode yang tidak diharapkan.
  const errorMessage =
    error?.type === "lead_time"
      ? "Jadwal yang dipilih terlalu dekat. Minimal 60 menit sebelum konsultasi."
      : error?.type === "limit"
      ? "Batas 3 booking aktif tercapai. Batalkan salah satu booking yang ada terlebih dahulu."
      : error?.type === "already_booked"
      ? null
      : error?.type === "unknown"
      ? "Terjadi kesalahan. Silakan coba lagi."
      : error?.message ?? null;

  const topicLength = topic.length;
  const descLength = description.length;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <div className="relative">
        <DialogClose onClick={() => handleOpenChange(false)} />
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>Pesan Konsultasi</DialogTitle>
            <DialogDescription>
              {slot && (
                <>
                  {formatJakartaLongDate(slot.start_at)} ·{" "}
                  {formatJakartaTime(slot.start_at)}–{formatJakartaTime(slot.end_at)}
                  <br />
                  dengan {lecturerName}
                </>
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            {/* Error alert (hanya untuk error non-already_booked) */}
            {errorMessage && error && (
              <div
                className={`rounded-lg border px-4 py-3 text-sm ${
                  error.type === "limit"
                    ? "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200"
                    : "border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200"
                }`}
                role="alert"
              >
                {errorMessage}
                {error.type === "limit" && (
                  <div className="mt-2">
                    <Link
                      href="/booking-saya"
                      className="underline underline-offset-2"
                      onClick={() => handleOpenChange(false)}
                    >
                      Lihat Booking Saya
                    </Link>
                  </div>
                )}
              </div>
            )}

            {/* Topic */}
            <div className="space-y-1.5">
              <Label htmlFor="booking-topic">
                Topik <span className="text-destructive">*</span>
              </Label>
              <Input
                id="booking-topic"
                type="text"
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                placeholder="Contoh: Bimbingan proposal skripsi"
                minLength={3}
                maxLength={200}
                required
                disabled={isPending}
              />
              <p className="text-xs text-muted-foreground">
                {topicLength}/200 karakter (minimal 3)
              </p>
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <Label htmlFor="booking-description">Deskripsi</Label>
              <textarea
                id="booking-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Opsional: jelaskan secara singkat apa yang ingin dibahas"
                maxLength={2000}
                rows={3}
                className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                disabled={isPending}
              />
              <p className="text-xs text-muted-foreground">
                {descLength}/2000 karakter
              </p>
            </div>
          </div>

          <DialogFooter className="mt-6">
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={isPending}
            >
              Batal
            </Button>
            <Button type="submit" disabled={isPending || topic.trim().length < 3}>
              {isPending ? "Memproses..." : "Pesan"}
            </Button>
          </DialogFooter>
        </form>
      </div>
    </Dialog>
  );
}
