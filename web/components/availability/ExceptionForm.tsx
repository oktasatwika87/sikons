"use client";

import { useState } from "react";
import { useCreateAvailabilityException } from "@/hooks/useAvailability";
import { createExceptionSchema, type CreateExceptionValues } from "@/lib/availability/schemas";
import { ApiError } from "@/lib/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { SlotReconcileSummary } from "@/lib/api/types";

interface ExceptionFormProps {
  onSuccess: (summary: SlotReconcileSummary) => void;
  onCancel: () => void;
}

export function ExceptionForm({ onSuccess, onCancel }: ExceptionFormProps) {
  const createMutation = useCreateAvailabilityException();

  const [form, setForm] = useState<CreateExceptionValues>({
    exception_date: new Date().toISOString().slice(0, 10),
    reason: "",
    is_full_day: false,
    start_time: "09:00",
    end_time: "12:00",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  function handleChange(field: keyof CreateExceptionValues, value: string | boolean | number) {
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

    const parsed = createExceptionSchema.safeParse(form);
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
        if (err.is("VALIDATION_ERROR")) {
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

      {/* Tanggal */}
      <div>
        <Label htmlFor="exception_date">Tanggal</Label>
        <Input
          id="exception_date"
          type="date"
          value={form.exception_date}
          onChange={(e) => handleChange("exception_date", e.target.value)}
          className={errors.exception_date ? "border-destructive" : ""}
        />
        {errors.exception_date && (
          <p className="mt-1 text-xs text-destructive">{errors.exception_date}</p>
        )}
      </div>

      {/* Alasan */}
      <div>
        <Label htmlFor="reason">Alasan</Label>
        <Input
          id="reason"
          type="text"
          value={form.reason}
          onChange={(e) => handleChange("reason", e.target.value)}
          placeholder="Libur nasional, meetings, dll."
          className={errors.reason ? "border-destructive" : ""}
        />
        {errors.reason && (
          <p className="mt-1 text-xs text-destructive">{errors.reason}</p>
        )}
      </div>

      {/* Full day toggle */}
      <div className="flex items-center gap-3">
        <input
          id="is_full_day"
          type="checkbox"
          checked={form.is_full_day}
          onChange={(e) => handleChange("is_full_day", e.target.checked)}
          className="h-4 w-4 rounded border-input"
        />
        <Label htmlFor="is_full_day" className="cursor-pointer">
          Libur seharian
        </Label>
      </div>

      {/* Jam (jika bukan full day) */}
      {!form.is_full_day && (
        <div className="grid grid-cols-2 gap-4">
          <div>
            <Label htmlFor="start_time">Jam Mulai</Label>
            <Input
              id="start_time"
              type="time"
              value={form.start_time ?? ""}
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
              value={form.end_time ?? ""}
              onChange={(e) => handleChange("end_time", e.target.value)}
              className={errors.end_time ? "border-destructive" : ""}
            />
            {errors.end_time && (
              <p className="mt-1 text-xs text-destructive">{errors.end_time}</p>
            )}
          </div>
        </div>
      )}

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
