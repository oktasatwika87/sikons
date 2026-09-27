/**
 * Custom error class untuk error API.
 *
 * Format error backend:
 * { "error": { "code": "SLOT_ALREADY_BOOKED", "message": "...", "details": {} } }
 *
 * `code` adalah kontrak untuk mesin — frontend mencabangkan logika di sini.
 */
export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly details?: Record<string, unknown>
  ) {
    super(message);
    this.name = "ApiError";
  }

  is(code: string): boolean {
    return this.code === code;
  }
}

/**
 * Error mutasi yang sudah dipetakan: `type` untuk percabangan di UI,
 * `message` dari backend untuk ditampilkan apa adanya.
 */
export interface TypedError<T extends string> {
  type: T | "unknown";
  message: string;
}

/**
 * Petakan error dari apiFetch ke TypedError berdasarkan `code` backend.
 *
 * Panggil di dalam `mutationFn` lalu lempar hasilnya. Jangan di `onError`:
 * TanStack Query mengabaikan nilai kembalian `onError`, jadi komponen tetap
 * menerima ApiError mentah dan `error.type` selalu undefined.
 */
export function toTypedError<T extends string>(
  err: unknown,
  codes: Record<string, T>
): TypedError<T> {
  if (err instanceof ApiError) {
    return { type: codes[err.code] ?? "unknown", message: err.message };
  }
  return {
    type: "unknown",
    message: err instanceof Error ? err.message : "Terjadi kesalahan",
  };
}
