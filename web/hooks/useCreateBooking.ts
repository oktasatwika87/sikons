"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { ApiError } from "@/lib/api/errors";
import type { BookingResponse } from "@/lib/api/types";

export interface CreateBookingError {
  type: "already_booked" | "lead_time" | "limit" | "unknown";
  message: string;
}

interface CreateBookingVariables {
  slotId: string;
  topic: string;
  description: string;
}

export function useCreateBooking(lecturerId: string) {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();

  // Idempotency key dibuat sekali saat hook mount.
  // Dialog tertutup -> hook di-unmount -> key hilang.
  // Dialog terbuka lagi -> key baru dibuat.
  //
  // Server menghitung dan memverifikasi request_hash dari body yang diterimanya.
  // Frontend tidak perlu mengirim hash -- server menghitungnya sendiri.
  const [idempotencyKey] = useState(() => crypto.randomUUID());

  const lecturerIdRef = useRef(lecturerId);
  // Sync ref dengan prop terbaru.
  useEffect(() => { lecturerIdRef.current = lecturerId; }, [lecturerId]);

  const mutation = useMutation<BookingResponse, CreateBookingError, CreateBookingVariables>({
    mutationFn: async ({ slotId, topic, description }) => {
      return fetchWithRetry<BookingResponse>("/bookings", {
        method: "POST",
        headers: {
          "Idempotency-Key": idempotencyKey,
        },
        body: JSON.stringify({
          slot_id: slotId,
          topic,
          description,
        }),
      });
    },
    onError: (error) => {
      if (error instanceof ApiError) {
        switch (error.code) {
          case "SLOT_ALREADY_BOOKED":
            queryClient.invalidateQueries({
              queryKey: ["lecturer", lecturerIdRef.current, "slots"],
            });
            return { type: "already_booked" as const, message: error.message };

          case "SLOT_TOO_SOON":
            return { type: "lead_time" as const, message: error.message };

          case "BOOKING_LIMIT_REACHED":
            return { type: "limit" as const, message: error.message };

          // Idempotency key reused dengan body berbeda — indicates client bug
          // (frontend tidak mengirim request_hash, jadi ini tidak mungkin terjadi
          // dengan implementasi normal). Fallback ke unknown.
          case "IDEMPOTENCY_KEY_REUSED":
            return { type: "unknown" as const, message: error.message };

          default:
            return { type: "unknown" as const, message: error.message };
        }
      }
      return {
        type: "unknown" as const,
        message: error instanceof Error ? error.message : "Terjadi kesalahan",
      };
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["bookings"] });
      queryClient.invalidateQueries({
        queryKey: ["lecturer", lecturerIdRef.current, "slots"],
      });
    },
  });

  const resetError = useCallback(() => {
    mutation.reset();
  }, [mutation]);

  return { ...mutation, resetError, idempotencyKey };
}
