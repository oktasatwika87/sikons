/**
 * Tipe TypeScript untuk semua DTO yang dikirim/diterima API backend.
 *
 * Didefinisikan langsung dari struct Go yang sudah ada:
 * - userResponse di internal/server/auth_handlers.go
 * - Lecturer/Slot/ListResult di internal/lecturer/service.go
 * - bookingResponse di internal/server/booking_handlers.go
 * - Rule/Exception di internal/availability/service.go
 * - ErrorBody di internal/httpx/httpx.go
 */

// ------------------------------------------------------------------ Auth

export interface UserResponse {
  id: string;
  email: string;
  full_name: string;
  role: "student" | "lecturer" | "admin";
  identity_number?: string;
  is_active: boolean;
  created_at: string; // RFC3339
}

export interface RegisterRequest {
  email: string;
  password: string;
  full_name: string;
  identity_number: string;
}

export interface LoginResponse {
  user: UserResponse;
  access_token: string;
  expires_at: string; // RFC3339
  refresh_expires_at: string; // RFC3339
}

export interface RefreshResponse {
  access_token: string;
  expires_at: string; // RFC3339
}

// ------------------------------------------------------------------ Lecturer

export interface Lecturer {
  id: string;
  full_name: string;
  department: string;
  room: string;
  bio: string;
}

export interface LecturerListResponse {
  lecturers: Lecturer[];
  page: number;
  per_page: number;
  total: number;
}

export interface Slot {
  id: string;
  start_at: string; // RFC3339
  end_at: string;   // RFC3339
  status: string;   // "open" | "booked" | "available"
}

export interface LecturerSlotsResponse {
  lecturer_id: string;
  from: string; // RFC3339
  to: string;   // RFC3339
  slots: Slot[];
}

// ------------------------------------------------------------------ Booking

export interface SlotRef {
  id: string;
  start_at: string; // RFC3339
  end_at: string;   // RFC3339
}

export interface PersonRef {
  id: string;
  full_name: string;
  department?: string;
  identity_number?: string;
}

export interface BookingResponse {
  id: string;
  status: "confirmed" | "cancelled" | "completed" | "no_show";
  topic: string;
  description: string;
  created_at: string; // RFC3339
  slot: SlotRef;
  lecturer?: PersonRef;
  student?: PersonRef;
}

export interface BookingListResponse {
  bookings: BookingResponse[];
  page: number;
  per_page: number;
  total: number;
}

export interface CreateBookingRequest {
  slot_id: string;
  topic: string;
  description: string;
}

export interface CancelBookingRequest {
  reason?: string;
}

export interface CompleteBookingRequest {
  lecturer_note?: string;
}

// ------------------------------------------------------------------ Availability

export interface AvailabilityRule {
  id: string;
  lecturer_id: string;
  day_of_week: number; // 0=Minggu, 6=Sabtu
  start_time: string;  // HH:MM
  end_time: string;    // HH:MM
  slot_duration_min: number;
  effective_from: string;    // YYYY-MM-DD
  effective_to?: string;     // YYYY-MM-DD, nullable
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface AvailabilityException {
  id: string;
  lecturer_id: string;
  exception_date: string; // YYYY-MM-DD
  reason: string;
  is_full_day: boolean;
  start_time?: string; // HH:MM, nullable
  end_time?: string;   // HH:MM, nullable
  created_at: string;
}

export interface CreateRuleRequest {
  day_of_week: number;
  start_time: string; // HH:MM
  end_time: string;   // HH:MM
  slot_duration_min: number;
  effective_from: string;    // YYYY-MM-DD
  effective_to?: string;   // YYYY-MM-DD, opsional
}

export interface UpdateRuleRequest {
  effective_to?: string; // YYYY-MM-DD
  is_active?: boolean;
}

export interface CreateExceptionRequest {
  exception_date: string; // YYYY-MM-DD
  reason: string;
  is_full_day: boolean;
  start_time?: string; // HH:MM, opsional
  end_time?: string;   // HH:MM, opsional
}

export interface SlotReconcileSummary {
  created: number;
  deleted: number;
  withdrawn: number;
  restored: number;
  bookings_cancelled: number;
}

export interface RuleWithSlots {
  id: string;
  slots: SlotReconcileSummary;
}

export interface ExceptionWithSlots {
  id: string;
  slots: SlotReconcileSummary;
}

// ------------------------------------------------------------------ Error

export interface ErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface ErrorEnvelope {
  error: ErrorBody;
}
