"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { toTypedError, type TypedError } from "@/lib/api/errors";
import type { BookingResponse } from "@/lib/api/types";

export type NoShowBookingError = TypedError<
  "not_found" | "not_confirmed" | "already_finalized"
>;

const NO_SHOW_CODES = {
  BOOKING_NOT_FOUND: "not_found",
  BOOKING_NOT_CONFIRMED: "not_confirmed",
  BOOKING_ALREADY_FINALIZED: "already_finalized",
} as const;

export function useNoShowBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<BookingResponse, NoShowBookingError, { bookingId: string }>({
    mutationFn: async ({ bookingId }) => {
      try {
        return await fetchWithRetry<BookingResponse>(
          `/bookings/${bookingId}/no-show`,
          { method: "PATCH" }
        );
      } catch (err) {
        throw toTypedError(err, NO_SHOW_CODES);
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
    },
  });
}
