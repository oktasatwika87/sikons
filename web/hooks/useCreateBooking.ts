"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { toTypedError, type TypedError } from "@/lib/api/errors";
import type { BookingResponse } from "@/lib/api/types";

export type CreateBookingError = TypedError<"already_booked" | "lead_time" | "limit">;

// IDEMPOTENCY_KEY_REUSED sengaja tidak dipetakan (jatuh ke "unknown"): itu
// tanda bug di klien, bukan kondisi yang bisa diperbaiki pengguna.
const CREATE_CODES = {
  SLOT_ALREADY_BOOKED: "already_booked",
  SLOT_TOO_SOON: "lead_time",
  BOOKING_LIMIT_REACHED: "limit",
} as const;

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
      try {
        return await fetchWithRetry<BookingResponse>("/bookings", {
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
      } catch (err) {
        throw toTypedError(err, CREATE_CODES);
      }
    },
    onError: (error) => {
      // Slot sudah diambil orang lain: kalender perlu dimuat ulang supaya
      // slot itu tampil sebagai booked.
      if (error.type === "already_booked") {
        queryClient.invalidateQueries({
          queryKey: ["lecturer", lecturerIdRef.current, "slots"],
        });
      }
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
