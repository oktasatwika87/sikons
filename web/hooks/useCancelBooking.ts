"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { toTypedError, type TypedError } from "@/lib/api/errors";
import type { BookingResponse } from "@/lib/api/types";

export type CancelBookingError = TypedError<
  "not_found" | "too_late" | "already_cancelled" | "already_finalized"
>;

const CANCEL_CODES = {
  BOOKING_NOT_FOUND: "not_found",
  CANCEL_TOO_LATE: "too_late",
  BOOKING_ALREADY_CANCELLED: "already_cancelled",
  BOOKING_ALREADY_FINALIZED: "already_finalized",
} as const;

export function useCancelBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<
    BookingResponse,
    CancelBookingError,
    { bookingId: string; reason?: string }
  >({
    mutationFn: async ({ bookingId, reason }) => {
      try {
        return await fetchWithRetry<BookingResponse>(
          `/bookings/${bookingId}/cancel`,
          {
            method: "PATCH",
            body: JSON.stringify({ reason }),
          }
        );
      } catch (err) {
        throw toTypedError(err, CANCEL_CODES);
      }
    },
    onSuccess: () => {
      // Invalidate semua query booking.
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
    },
  });
}
