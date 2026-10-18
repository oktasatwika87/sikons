"use client";

import {
  Dialog,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

interface AlertDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  cancelText?: string;
  confirmText?: string;
  onConfirm: () => void;
  variant?: "default" | "destructive";
  loading?: boolean;
  /** Pesan gagal dari aksi terakhir; dialog tetap terbuka supaya bisa dibaca. */
  error?: string | null;
}

export function AlertDialog({
  open,
  onOpenChange,
  title,
  description,
  cancelText = "Batal",
  confirmText = "Konfirmasi",
  onConfirm,
  variant = "default",
  loading = false,
  error = null,
}: AlertDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <div className="relative">
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1">
            <DialogTitle>{title}</DialogTitle>
            {description && (
              <DialogDescription className="mt-2">
                {description}
              </DialogDescription>
            )}
          </div>
          <DialogClose onClick={() => onOpenChange(false)} />
        </div>
        {error && (
          <p
            role="alert"
            className="mt-4 rounded-lg border border-destructive bg-destructive-muted px-4 py-3 text-sm text-destructive"
          >
            {error}
          </p>
        )}

        <DialogFooter className="mt-6">
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
            type="button"
          >
            {cancelText}
          </Button>
          <Button
            variant={variant === "destructive" ? "destructive" : "default"}
            onClick={onConfirm}
            disabled={loading}
            type="button"
          >
            {loading ? "Memproses..." : confirmText}
          </Button>
        </DialogFooter>
      </div>
    </Dialog>
  );
}
