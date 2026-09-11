"use client";

import { useState, useEffect } from "react";
import { useCreateAvailabilityRule } from "@/hooks/useAvailability";
import { createRuleSchema, calcSlotCount, type CreateRuleValues } from "@/lib/availability/schemas";
import { ApiError } from "@/lib/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { SlotReconcileSummary } from "@/lib/api/types";

interface RuleFormProps {
  onSuccess: (summary: SlotReconcileSummary) => void;
  onCancel: () => void;
}

const DAY_OPTIONS = [
  { value: 1, label: "Senin" },
  { value: 2, label: "Selasa" },
  { value: 3, label: "Rabu" },
  { value: 4, label: "Kamis" },
  { value: 5, label: "Jumat" },
  { value: 6, label: "Sabtu" },
  { value: 0, label: "Minggu" },
];

export function RuleForm({ onSuccess, onCancel }: RuleFormProps) {
  const createMutation = useCreateAvailabilityRule();

  const [form, setForm] = useState<CreateRuleValues>({
    day_of_week: 1,
    start_time: "09:00",
    end_time: "12:00",
    slot_duration_min: 30,
    effective_from: new Date().toISOString().slice(0, 10),
    effective_to: undefined,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  const parsed = createRuleSchema.safeParse(form);
  const slotCount = parsed.success ? calcSlotCount(parsed.data) : 0;

  function handleChange(field: keyof CreateRuleValues, value: string | number | undefined) {
    setForm((prev) => ({ ...prev, [field]: value }));
    setErrors((prev) => {
      const next = { ...prev };
      delete next[field];
      return next;
    });
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    const parsed = createRuleSchema.safeParse(form);
    if (!parsed.success) {
      const fieldErrors: Record<string, string> = {};
      for (const issue of parsed.error.issues) {
        const key = String(issue.path[0]);
        if (!fieldErrors[key]) {
          fieldErrors[key] = issue.message;
        }
      }
      setErrors(fieldErrors);
      return;
    }

    try {
      const result = await createMutation.mutateAsync(parsed.data);
      onSuccess(result.slots);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.is("ATURAN_BENTROK")) {
          setError("Sudah ada aturan aktif di hari dan rentang jam yang sama.");
        } else if (err.is("VALIDATION_ERROR")) {
          setError(err.message);
        } else {
          setError("Terjadi kesalahan. Silakan coba lagi.");
        }
      } else {
        setError("Terjadi kesalahan.");
      }
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {error && (
        <div className="rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      )}

      {/* Hari */}
      <div>
        <Label htmlFor="day_of_week">Hari</Label>
        <select
          id="day_of_week"
          value={form.day_of_week}
          onChange={(e) => handleChange("day_of_week", parseInt(e.target.value))}
          className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        >
          {DAY_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>

      {/* Jam mulai & selesai */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label htmlFor="start_time">Jam Mulai</Label>
          <Input
            id="start_time"
            type="time"
            value={form.start_time}
            onChange={(e) => handleChange("start_time", e.target.value)}
            className={errors.start_time ? "border-destructive" : ""}
          />
          {errors.start_time && (
            <p className="mt-1 text-xs text-destructive">{errors.start_time}</p>
          )}
        </div>
        <div>
          <Label htmlFor="end_time">Jam Selesai</Label>
          <Input
            id="end_time"
            type="time"
            value={form.end_time}
            onChange={(e) => handleChange("end_time", e.target.value)}
            className={errors.end_time ? "border-destructive" : ""}
          />
          {errors.end_time && (
            <p className="mt-1 text-xs text-destructive">{errors.end_time}</p>
          )}
        </div>
      </div>

      {/* Durasi slot */}
      <div>
        <Label htmlFor="slot_duration_min">Durasi per Sesi (menit)</Label>
        <Input
          id="slot_duration_min"
          type="number"
          min={15}
          max={180}
          value={form.slot_duration_min}
          onChange={(e) => handleChange("slot_duration_min", parseInt(e.target.value))}
          className={errors.slot_duration_min ? "border-destructive" : ""}
        />
        {errors.slot_duration_min && (
          <p className="mt-1 text-xs text-destructive">{errors.slot_duration_min}</p>
        )}
      </div>

      {/* Preview slot count */}
      {parsed.success && slotCount > 0 && (
        <p className="text-sm text-muted-foreground">
          {form.start_time}–{form.end_time}, sesi {form.slot_duration_min} menit →{" "}
          <strong>{slotCount} slot</strong> per{" "}
          {DAY_OPTIONS.find((d) => d.value === form.day_of_week)?.label}
        </p>
      )}
      {errors.slot_duration_min && !form.start_time.includes("") && (
        <p className="text-sm text-muted-foreground">
          {errors.slot_duration_min}
        </p>
      )}

      {/* Tanggal berlaku */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label htmlFor="effective_from">Berlaku Dari</Label>
          <Input
            id="effective_from"
            type="date"
            value={form.effective_from}
            onChange={(e) => handleChange("effective_from", e.target.value)}
            className={errors.effective_from ? "border-destructive" : ""}
          />
          {errors.effective_from && (
            <p className="mt-1 text-xs text-destructive">{errors.effective_from}</p>
          )}
        </div>
        <div>
          <Label htmlFor="effective_to">Berlaku Sampai (opsional)</Label>
          <Input
            id="effective_to"
            type="date"
            value={form.effective_to ?? ""}
            onChange={(e) =>
              handleChange("effective_to", e.target.value || undefined)
            }
            className={errors.effective_to ? "border-destructive" : ""}
          />
          {errors.effective_to && (
            <p className="mt-1 text-xs text-destructive">{errors.effective_to}</p>
          )}
        </div>
      </div>

      {/* Action buttons */}
      <div className="flex justify-end gap-3 pt-2">
        <Button variant="outline" onClick={onCancel} type="button">
          Batal
        </Button>
        <Button type="submit" disabled={createMutation.isPending}>
          {createMutation.isPending ? "Menyimpan..." : "Simpan"}
        </Button>
      </div>
    </form>
  );
}
