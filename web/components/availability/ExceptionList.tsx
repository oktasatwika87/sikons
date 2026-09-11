"use client";

import { useState } from "react";
import { useAvailabilityExceptions, useDeleteAvailabilityException } from "@/hooks/useAvailability";
import { AlertDialog } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api/errors";
import type { AvailabilityException, SlotReconcileSummary } from "@/lib/api/types";

interface ExceptionListProps {
  from: string;
  to: string;
  onSummary: (summary: SlotReconcileSummary) => void;
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00Z");
  return d.toLocaleDateString("id-ID", {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
}

export function ExceptionList({ from, to, onSummary }: ExceptionListProps) {
  const { data: exceptions, isLoading, isError } = useAvailabilityExceptions(from, to);
  const deleteMutation = useDeleteAvailabilityException();

  const [confirmTarget, setConfirmTarget] = useState<AvailabilityException | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="space-y-2">
        {[1, 2].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded-lg bg-muted" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <p className="text-sm text-destructive">Gagal memuat pengecualian.</p>
    );
  }

  if (!exceptions || exceptions.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        Tidak ada pengecualian dalam rentang ini.
      </p>
    );
  }

  return (
    <>
      {error && (
        <div className="mb-4 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="space-y-2">
        {exceptions.map((exc) => (
          <div
            key={exc.id}
            className="flex items-start justify-between gap-4 rounded-lg border border-border px-4 py-3"
          >
            <div className="flex-1">
              <p className="font-medium">{formatDate(exc.exception_date)}</p>
              <p className="mt-0.5 text-sm text-muted-foreground">{exc.reason}</p>
              {!exc.is_full_day && exc.start_time && exc.end_time && (
                <p className="mt-1 text-sm text-muted-foreground">
                  {exc.start_time}–{exc.end_time}
                </p>
              )}
              {exc.is_full_day && (
                <span className="mt-1 inline-block rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                  Seharian
                </span>
              )}
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setConfirmTarget(exc)}
              type="button"
              className="text-destructive hover:text-destructive shrink-0"
            >
              Hapus
            </Button>
          </div>
        ))}
      </div>

      {confirmTarget && (
        <ConfirmExceptionDialog
          exception={confirmTarget}
          loading={deleteMutation.isPending}
          onConfirm={async () => {
            setError(null);
            try {
              const result = await deleteMutation.mutateAsync(confirmTarget.id);
              onSummary(result.slots);
              setConfirmTarget(null);
            } catch (err) {
              if (err instanceof ApiError) {
                if (err.is("NOT_FOUND")) {
                  setError("Pengecualian tidak ditemukan.");
                } else {
                  setError(err.message);
                }
              } else {
                setError("Terjadi kesalahan.");
              }
            }
          }}
          onCancel={() => setConfirmTarget(null)}
        />
      )}
    </>
  );
}

interface ConfirmExceptionDialogProps {
  exception: AvailabilityException;
  loading: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

function ConfirmExceptionDialog({
  exception,
  loading,
  onConfirm,
  onCancel,
}: ConfirmExceptionDialogProps) {
  function formatDate(dateStr: string): string {
    const d = new Date(dateStr + "T00:00:00Z");
    return d.toLocaleDateString("id-ID", {
      day: "numeric",
      month: "short",
      year: "numeric",
      timeZone: "UTC",
    });
  }

  return (
    <AlertDialog
      open
      onOpenChange={(open) => !open && onCancel()}
      title="Hapus Pengecualian?"
      description={
        `Hapus pengecualian pada ${formatDate(exception.exception_date)} "${exception.reason}"? ` +
        `Tindakan ini bisa membatalkan booking yang sudah ada.`
      }
      cancelText="Batal"
      confirmText="Hapus"
      variant="destructive"
      loading={loading}
      onConfirm={onConfirm}
    />
  );
}
