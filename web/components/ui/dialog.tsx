"use client";

import { cn } from "cn";
import { X } from "lucide-react";

function Dialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
}) {
  if (!open) return null;
  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm"
        onClick={() => onOpenChange(false)}
      />
      {/* Panel */}
      <div className="fixed inset-0 z-50 flex items-center justify-center">
        <div
          className="relative z-50 w-full max-w-lg rounded-xl border border-border bg-background p-6 shadow-lg"
          role="dialog"
          aria-modal="true"
        >
          {children}
        </div>
      </div>
    </>
  );
}

const DialogContent = ({
  className,
  children,
  ...props
}: React.ComponentProps<"div">) => (
  <div className={cn("mt-4 space-y-4", className)} {...props}>
    {children}
  </div>
);
DialogContent.displayName = "DialogContent";

const DialogHeader = ({
  className,
  ...props
}: React.ComponentProps<"div">) => (
  <div
    className={cn("flex flex-col gap-1.5 text-center sm:text-left", className)}
    {...props}
  />
);
DialogHeader.displayName = "DialogHeader";

const DialogFooter = ({
  className,
  ...props
}: React.ComponentProps<"div">) => (
  <div
    className={cn(
      "flex flex-col-reverse sm:flex-row sm:justify-end sm:gap-2",
      className
    )}
    {...props}
  />
);
DialogFooter.displayName = "DialogFooter";

const DialogTitle = ({
  className,
  children,
  ...props
}: React.ComponentProps<"h2">) => (
  <h2 className={cn("text-lg font-semibold", className)} {...props}>
    {children}
  </h2>
);
DialogTitle.displayName = "DialogTitle";

const DialogDescription = ({
  className,
  ...props
}: React.ComponentProps<"p">) => (
  <p className={cn("text-sm text-muted-foreground", className)} {...props} />
);
DialogDescription.displayName = "DialogDescription";

const DialogClose = ({
  className,
  onClick,
  ...props
}: React.ComponentProps<"button"> & { onClick?: () => void }) => (
  <button
    type="button"
    className={cn(
      "absolute right-4 top-4 rounded-md p-1 text-muted-foreground opacity-70 transition-opacity hover:opacity-100",
      "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring",
      "disabled:pointer-events-none",
      className
    )}
    onClick={onClick}
    aria-label="Close"
    {...props}
  >
    <X className="h-4 w-4" />
  </button>
);
DialogClose.displayName = "DialogClose";

export {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
  DialogClose,
};
