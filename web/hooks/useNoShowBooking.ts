"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { ApiError } from "@/lib/api/errors";
import type { BookingResponse } from "@/lib/api/types";

export interface NoShowBookingError {
  type: "not_found" | "not_confirmed" | "already_finalized" | "unknown";
  message: string;
}

export function useNoShowBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<BookingResponse, NoShowBookingError, { bookingId: string }>({
    mutationFn: async ({ bookingId }) => {
      return fetchWithRetry<BookingResponse>(`/bookings/${bookingId}/no-show`, {
        method: "PATCH",
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
          case "BOOKING_NOT_CONFIRMED":
            return {
              type: "not_confirmed" as const,
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
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
    },
  });
}
