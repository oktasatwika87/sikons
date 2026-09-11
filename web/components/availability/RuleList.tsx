"use client";

import { useState } from "react";
import { useAvailabilityRules, useUpdateAvailabilityRule, useDeleteAvailabilityRule } from "@/hooks/useAvailability";
import { AlertDialog } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api/errors";
import type { AvailabilityRule, SlotReconcileSummary } from "@/lib/api/types";

interface RuleListProps {
  onSummary: (summary: SlotReconcileSummary) => void;
}

const DAY_NAMES: Record<number, string> = {
  0: "Minggu",
  1: "Senin",
  2: "Selasa",
  3: "Rabu",
  4: "Kamis",
  5: "Jumat",
  6: "Sabtu",
};

function formatTimeRange(rule: AvailabilityRule): string {
  return `${rule.start_time}–${rule.end_time}`;
}

function formatEffectiveDate(effectiveFrom: string, effectiveTo?: string): string {
  const from = formatDate(effectiveFrom);
  if (!effectiveTo) return `dari ${from}`;
  return `${from} – ${formatDate(effectiveTo)}`;
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00Z");
  return d.toLocaleDateString("id-ID", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });
}

export function RuleList({ onSummary }: RuleListProps) {
  const { data: rules, isLoading, isError } = useAvailabilityRules();
  const updateMutation = useUpdateAvailabilityRule();
  const deleteMutation = useDeleteAvailabilityRule();

  const [confirmTarget, setConfirmTarget] = useState<{
    rule: AvailabilityRule;
    action: "end" | "delete";
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (isLoading) {
    return (
      <div className="space-y-2">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-16 animate-pulse rounded-lg bg-muted" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <p className="text-sm text-destructive">Gagal memuat jadwal rutin.</p>
    );
  }

  if (!rules || rules.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        Belum ada jadwal rutin.
      </p>
    );
  }

  // Kelompokkan per hari.
  const byDay = new Map<number, AvailabilityRule[]>();
  for (const rule of rules) {
    const list = byDay.get(rule.day_of_week);
    if (list) list.push(rule);
    else byDay.set(rule.day_of_week, [rule]);
  }

  const sortedDays = Array.from(byDay.keys()).sort();

  return (
    <>
      {error && (
        <div className="mb-4 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="space-y-6">
        {sortedDays.map((day) => (
          <div key={day}>
            <h3 className="mb-2 text-sm font-medium text-muted-foreground">
              {DAY_NAMES[day]}
            </h3>
            <div className="space-y-2">
              {(byDay.get(day) ?? []).map((rule) => (
                <div
                  key={rule.id}
                  className={`flex items-center justify-between rounded-lg border px-4 py-3 ${
                    rule.is_active
                      ? "border-border"
                      : "border-muted bg-muted/30 opacity-60"
                  }`}
                >
                  <div className="flex-1">
                    <p className="font-medium">
                      {formatTimeRange(rule)}{" "}
                      <span className="text-sm font-normal text-muted-foreground">
                        ({rule.slot_duration_min} menit/slot)
                      </span>
                    </p>
                    <p className="mt-0.5 text-sm text-muted-foreground">
                      {formatEffectiveDate(rule.effective_from, rule.effective_to)}
                    </p>
                    {!rule.is_active && (
                      <span className="mt-1 inline-block rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                        Nonaktif
                      </span>
                    )}
                  </div>
                  <div className="ml-4 flex gap-2">
                    {rule.is_active && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setConfirmTarget({ rule, action: "end" })}
                        type="button"
                      >
                        Akhiri
                      </Button>
                    )}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setConfirmTarget({ rule, action: "delete" })}
                      type="button"
                      className="text-destructive hover:text-destructive"
                    >
                      Hapus
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>

      {/* Dialog konfirmasi */}
      {confirmTarget && (
        <ConfirmRuleDialog
          rule={confirmTarget.rule}
          action={confirmTarget.action}
          loading={updateMutation.isPending || deleteMutation.isPending}
          onConfirm={async () => {
            setError(null);
            try {
              let result: { slots: SlotReconcileSummary };
              if (confirmTarget.action === "end") {
                // Pakai effective_to = hari ini (YYYY-MM-DD).
                const today = new Date().toISOString().slice(0, 10);
                result = await updateMutation.mutateAsync({
                  id: confirmTarget.rule.id,
                  body: { effective_to: today },
                });
              } else {
                result = await deleteMutation.mutateAsync(confirmTarget.rule.id);
              }
              onSummary(result.slots);
              setConfirmTarget(null);
            } catch (err) {
              if (err instanceof ApiError) {
                if (err.is("NOT_FOUND")) {
                  setError("Aturan tidak ditemukan.");
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

interface ConfirmRuleDialogProps {
  rule: AvailabilityRule;
  action: "end" | "delete";
  loading: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

function ConfirmRuleDialog({
  rule,
  action,
  loading,
  onConfirm,
  onCancel,
}: ConfirmRuleDialogProps) {
  const isEnd = action === "end";
  const today = new Date().toISOString().slice(0, 10);

  return (
    <AlertDialog
      open
      onOpenChange={(open) => !open && onCancel()}
      title={isEnd ? "Akhiri Aturan?" : "Hapus Aturan?"}
      description={
        isEnd
          ? `Aksi ini akan mengakhiri jadwal ${DAY_NAMES[rule.day_of_week]} ${rule.start_time}–${rule.end_time} mulai hari ini (${formatDate(today)}). ` +
            `Slot yang sudah dibuat dari aturan ini akan diproses ulang. ` +
            `Tindakan ini bisa membatalkan booking yang sudah ada.`
          : `Hapus jadwal ${DAY_NAMES[rule.day_of_week]} ${rule.start_time}–${rule.end_time}? ` +
            `Slot yang dibuat dari aturan ini akan diproses ulang. ` +
            `Tindakan ini bisa membatalkan booking yang sudah ada.`
      }
      cancelText="Batal"
      confirmText={isEnd ? "Akhiri" : "Hapus"}
      variant="destructive"
      loading={loading}
      onConfirm={onConfirm}
    />
  );
}
