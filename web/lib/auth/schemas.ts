import { z } from "zod";

/** Email wajib "@" — sesuai validasi backend (strings.Contains). */
export const loginSchema = z.object({
  email: z.string().min(1, "Email wajib diisi"),
  password: z.string().min(1, "Password wajib diisi"),
});

export type LoginValues = z.infer<typeof loginSchema>;

/**
 * Register — field persis sama dengan registerRequest di backend:
 * email, password, full_name, identity_number.
 * Tidak ada field role (hardcoded 'student' di backend).
 *
 * Confirm password adalah field UI murni, tidak dikirim ke backend.
 * identity_number tanpa format baku — backend tidak punya validator.
 */
export const registerSchema = z
  .object({
    email: z.string().min(1, "Email wajib diisi"),
    password: z
      .string()
      .min(8, "Password minimal 8 karakter")
      .max(72, "Password maksimal 72 karakter"),
    confirmPassword: z.string().min(1, "Konfirmasi password wajib diisi"),
    full_name: z.string().min(1, "Nama lengkap wajib diisi").trim(),
    identity_number: z.string().min(1, "Nomor identitas wajib diisi"),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: "Password dan konfirmasi tidak cocok",
    path: ["confirmPassword"],
  });

export type RegisterValues = z.infer<typeof registerSchema>;
