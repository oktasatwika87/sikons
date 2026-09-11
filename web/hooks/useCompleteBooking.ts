"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { ApiError } from "@/lib/api/errors";
import type { BookingResponse, CompleteBookingRequest } from "@/lib/api/types";

export interface CompleteBookingError {
  type: "not_found" | "session_not_started" | "not_confirmed" | "already_finalized" | "unknown";
  message: string;
}

export function useCompleteBooking() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  return useMutation<BookingResponse, CompleteBookingError, { bookingId: string; lecturerNote?: string }>({
    mutationFn: async ({ bookingId, lecturerNote }) => {
      const body: CompleteBookingRequest = {};
      if (lecturerNote !== undefined) {
        body.lecturer_note = lecturerNote;
      }
      return fetchWithRetry<BookingResponse>(`/bookings/${bookingId}/complete`, {
        method: "PATCH",
        body: JSON.stringify(body),
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
          case "SESSION_NOT_STARTED":
            return {
              type: "session_not_started" as const,
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
