"use client";

import type { SlotReconcileSummary } from "@/lib/api/types";

interface ReconcileSummaryProps {
  summary: SlotReconcileSummary | null;
  onDismiss?: () => void;
}

/**
 * Panel ringkasan reconcile yang muncul setelah mutasi sukses.
 *
 * `bookings_cancelled > 0` diberi penekanan visual karena artinya ada
 * mahasiswa yang bookingnya ikut terbatalkan dan dosen mungkin perlu
 * ditindaklanjuti secara manual.
 */
export function ReconcileSummary({ summary, onDismiss }: ReconcileSummaryProps) {
  if (!summary) return null;

  const hasImpact =
    summary.created > 0 ||
    summary.deleted > 0 ||
    summary.withdrawn > 0 ||
    summary.restored > 0 ||
    summary.bookings_cancelled > 0;

  if (!hasImpact) return null;

  return (
    <div
      className={`rounded-lg border p-4 ${
        summary.bookings_cancelled > 0
          ? "border-destructive/50 bg-destructive/5"
          : "border-border bg-muted/50"
      }`}
      role="status"
      aria-live="polite"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium">
            {summary.bookings_cancelled > 0
              ? "Booking dibatalkan"
              : "Jadwal diperbarui"}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            Perubahan slot:
          </p>
          <div className="mt-2 flex flex-wrap gap-2">
            {summary.created > 0 && (
              <Badge variant="default">{summary.created} dibuat</Badge>
            )}
            {summary.deleted > 0 && (
              <Badge variant="default">{summary.deleted} dihapus</Badge>
            )}
            {summary.withdrawn > 0 && (
              <Badge variant="default">{summary.withdrawn} ditarik</Badge>
            )}
            {summary.restored > 0 && (
              <Badge variant="default">{summary.restored} dikembalikan</Badge>
            )}
            {summary.bookings_cancelled > 0 && (
              <Badge variant="destructive">
                {summary.bookings_cancelled} booking dibatalkan
              </Badge>
            )}
          </div>
        </div>
        {onDismiss && (
          <button
            onClick={onDismiss}
            className="text-sm text-muted-foreground hover:text-foreground"
            type="button"
          >
            Tutup
          </button>
        )}
      </div>
      {summary.bookings_cancelled > 0 && (
        <p className="mt-3 text-xs text-destructive">
          Beberapa booking mahasiswa ikut dibatalkan. Pertimbangkan untuk
          mengontak mereka secara langsung.
        </p>
      )}
    </div>
  );
}

function Badge({
  variant,
  children,
}: {
  variant: "default" | "destructive";
  children: React.ReactNode;
}) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
        variant === "destructive"
          ? "bg-destructive/20 text-destructive"
          : "bg-muted text-muted-foreground"
      }`}
    >
      {children}
    </span>
  );
}
