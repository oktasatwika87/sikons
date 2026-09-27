"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { toTypedError, type TypedError } from "@/lib/api/errors";
import type { BookingResponse, CompleteBookingRequest } from "@/lib/api/types";

export type CompleteBookingError = TypedError<
  "not_found" | "session_not_started" | "not_confirmed" | "already_finalized"
>;

const COMPLETE_CODES = {
  BOOKING_NOT_FOUND: "not_found",
  SESSION_NOT_STARTED: "session_not_started",
  BOOKING_NOT_CONFIRMED: "not_confirmed",
  BOOKING_ALREADY_FINALIZED: "already_finalized",
} as const;

export function useCompleteBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<BookingResponse, CompleteBookingError, { bookingId: string; lecturerNote?: string }>({
    mutationFn: async ({ bookingId, lecturerNote }) => {
      const body: CompleteBookingRequest = {};
      if (lecturerNote !== undefined) {
        body.lecturer_note = lecturerNote;
      }
      try {
        return await fetchWithRetry<BookingResponse>(`/bookings/${bookingId}/complete`, {
          method: "PATCH",
          body: JSON.stringify(body),
        });
      } catch (err) {
        throw toTypedError(err, COMPLETE_CODES);
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
    },
  });
}
