"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { ApiError } from "@/lib/api/errors";
import type { BookingResponse, CancelBookingRequest } from "@/lib/api/types";

export interface CancelBookingError {
  type: "not_found" | "too_late" | "already_cancelled" | "already_finalized" | "unknown";
  message: string;
}

export function useCancelBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<
    BookingResponse,
    CancelBookingError,
    { bookingId: string; reason?: string }
  >({
    mutationFn: async ({ bookingId, reason }) => {
      return fetchWithRetry<BookingResponse>(`/bookings/${bookingId}/cancel`, {
        method: "PATCH",
        body: JSON.stringify({
          reason,
        }),
      });
    },
    onError: (error) => {
      if (error instanceof ApiError) {
        switch (error.code) {
          case "BOOKING_NOT_FOUND":
            return {
              type: "not_found" as const,
              message: error.message,
            };
          case "CANCEL_TOO_LATE":
            return {
              type: "too_late" as const,
              message: error.message,
            };
          case "BOOKING_ALREADY_CANCELLED":
            return {
              type: "already_cancelled" as const,
              message: error.message,
            };
          case "BOOKING_ALREADY_FINALIZED":
            return {
              type: "already_finalized" as const,
              message: error.message,
            };
          default:
            return {
              type: "unknown" as const,
              message: error.message,
            };
        }
      }
      return {
        type: "unknown" as const,
        message: error instanceof Error ? error.message : "Terjadi kesalahan",
      };
    },
    onSuccess: () => {
      // Invalidate semua query booking.
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
    },
  });
}
