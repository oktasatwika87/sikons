"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { keepPreviousData } from "@tanstack/react-query";
import { useApiFetch } from "@/hooks/useApiFetch";
import { useAuth } from "@/lib/auth/AuthContext";
import { ApiError } from "@/lib/api/errors";
import type {
  AvailabilityRule,
  AvailabilityException,
  CreateRuleRequest,
  UpdateRuleRequest,
  CreateExceptionRequest,
  RuleWithSlots,
  ExceptionWithSlots,
  SlotReconcileSummary,
} from "@/lib/api/types";

function invalidateLecturerSlots(queryClient: ReturnType<typeof useQueryClient>, userId?: string) {
  if (userId) {
    queryClient.invalidateQueries({ queryKey: ["lecturer", userId, "slots"] });
  }
}

function invalidateAvailability(queryClient: ReturnType<typeof useQueryClient>) {
  queryClient.invalidateQueries({ queryKey: ["availability-rules"] });
  queryClient.invalidateQueries({ queryKey: ["availability-exceptions"] });
}

// ------------------------------------------------------------------ Rule mutations

export function useCreateAvailabilityRule() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();
  const { auth } = useAuth();

  return useMutation<RuleWithSlots, ApiError, CreateRuleRequest>({
    mutationFn: (body) =>
      fetchWithRetry<RuleWithSlots>("/availability-rules", {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      invalidateAvailability(queryClient);
      invalidateLecturerSlots(queryClient, auth.user?.id);
    },
  });
}

export function useUpdateAvailabilityRule() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();
  const { auth } = useAuth();

  return useMutation<
    { slots: SlotReconcileSummary },
    ApiError,
    { id: string; body: UpdateRuleRequest }
  >({
    mutationFn: ({ id, body }) =>
      fetchWithRetry<{ slots: SlotReconcileSummary }>(
        `/availability-rules/${id}`,
        { method: "PATCH", body }
      ),
    onSuccess: () => {
      invalidateAvailability(queryClient);
      invalidateLecturerSlots(queryClient, auth.user?.id);
    },
  });
}

export function useDeleteAvailabilityRule() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();
  const { auth } = useAuth();

  return useMutation<{ slots: SlotReconcileSummary }, ApiError, string>({
    mutationFn: (id) =>
      fetchWithRetry<{ slots: SlotReconcileSummary }>(
        `/availability-rules/${id}`,
        { method: "DELETE" }
      ),
    onSuccess: () => {
      invalidateAvailability(queryClient);
      invalidateLecturerSlots(queryClient, auth.user?.id);
    },
  });
}

// ------------------------------------------------------------------ Exception mutations

export function useCreateAvailabilityException() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();
  const { auth } = useAuth();

  return useMutation<ExceptionWithSlots, ApiError, CreateExceptionRequest>({
    mutationFn: (body) =>
      fetchWithRetry<ExceptionWithSlots>("/availability-exceptions", {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      invalidateAvailability(queryClient);
      invalidateLecturerSlots(queryClient, auth.user?.id);
    },
  });
}

export function useDeleteAvailabilityException() {
  const fetchWithRetry = useApiFetch();
  const queryClient = useQueryClient();
  const { auth } = useAuth();

  return useMutation<{ slots: SlotReconcileSummary }, ApiError, string>({
    mutationFn: (id) =>
      fetchWithRetry<{ slots: SlotReconcileSummary }>(
        `/availability-exceptions/${id}`,
        { method: "DELETE" }
      ),
    onSuccess: () => {
      invalidateAvailability(queryClient);
      invalidateLecturerSlots(queryClient, auth.user?.id);
    },
  });
}

// ------------------------------------------------------------------ Query hooks

export function useAvailabilityRules() {
  const fetchWithRetry = useApiFetch();

  return useQuery<AvailabilityRule[]>({
    queryKey: ["availability-rules"],
    queryFn: () =>
      fetchWithRetry<AvailabilityRule[]>("/availability-rules"),
    placeholderData: keepPreviousData,
  });
}

export function useAvailabilityExceptions(from: string, to: string) {
  const fetchWithRetry = useApiFetch();

  return useQuery<AvailabilityException[]>({
    queryKey: ["availability-exceptions", { from, to }],
    queryFn: () =>
      fetchWithRetry<AvailabilityException[]>(
        `/availability-exceptions?from=${from}&to=${to}`
      ),
    placeholderData: keepPreviousData,
    enabled: !!from && !!to,
  });
}
