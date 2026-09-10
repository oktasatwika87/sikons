import type { ErrorEnvelope } from "./types";
import { ApiError } from "./errors";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080/api/v1";

export class NetworkError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "NetworkError";
  }
}

/**
 * Fungsi fetch polos untuk API backend.
 *
 * Yang dilakukan:
 * - Base URL dari env NEXT_PUBLIC_API_URL
 * - credentials: 'include' selalu (cookie refresh butuh ini)
 * - Content-Type: application/json otomatis kalau body adalah object
 * - Parse error response ke ApiError
 */
export async function apiFetch<T>(
  path: string,
  options?: Omit<RequestInit, "body" | "headers"> & {
    body?: BodyInit | Record<string, unknown>;
    headers?: HeadersInit;
  }
): Promise<T> {
  const url = path.startsWith("http") ? path : `${BASE_URL}${path}`;

  const headers = new Headers(options?.headers as HeadersInit);

  let body: BodyInit | undefined = undefined;
  const rawBody = options?.body;
  if (rawBody !== undefined) {
    if (
      typeof rawBody === "object" &&
      rawBody !== null &&
      !(rawBody instanceof FormData) &&
      !(rawBody instanceof Blob) &&
      !(rawBody instanceof ArrayBuffer) &&
      !(rawBody instanceof URLSearchParams) &&
      !(rawBody instanceof ReadableStream)
    ) {
      body = JSON.stringify(rawBody);
      headers.set("Content-Type", "application/json");
    } else {
      body = rawBody as BodyInit;
    }
  }

  let response: Response;
  try {
    response = await fetch(url, {
      ...options,
      headers,
      body,
      credentials: "include",
    });
  } catch (err) {
    throw new NetworkError(
      err instanceof Error ? err.message : "Network request failed"
    );
  }

  // Handle 204 No Content
  if (response.status === 204) {
    return undefined as T;
  }

  // Parse response
  let data: unknown;
  try {
    data = await response.json();
  } catch {
    // Response bukan JSON — throw generic error
    if (!response.ok) {
      throw new ApiError(
        "RESPONSE_NOT_JSON",
        `Request failed with status ${response.status}`,
        { status: response.status }
      );
    }
    return undefined as T;
  }

  // Handle error response
  if (!response.ok) {
    const errEnvelope = data as ErrorEnvelope;
    throw new ApiError(
      errEnvelope.error?.code ?? "UNKNOWN_ERROR",
      errEnvelope.error?.message ?? `Request failed with status ${response.status}`,
      errEnvelope.error?.details
    );
  }

  return data as T;
}
