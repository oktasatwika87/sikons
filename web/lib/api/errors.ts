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
