import { describe, expect, it } from "vitest";
import { loginSchema, registerSchema } from "@/lib/auth/schemas";

describe("loginSchema", () => {
  it("valid: email + password terisi", () => {
    const result = loginSchema.safeParse({ email: "test@example.com", password: "password123" });
    expect(result.success).toBe(true);
  });

  it("invalid: email kosong", () => {
    const result = loginSchema.safeParse({ email: "", password: "password123" });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0].message).toBe("Email wajib diisi");
  });

  it("invalid: password kosong", () => {
    const result = loginSchema.safeParse({ email: "test@example.com", password: "" });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0].message).toBe("Password wajib diisi");
  });
});

describe("registerSchema", () => {
  it("valid: semua field terisi", () => {
    const result = registerSchema.safeParse({
      email: "student@uni.ac.id",
      password: "password123",
      confirmPassword: "password123",
      full_name: "Budi Santoso",
      identity_number: "1234567890",
    });
    expect(result.success).toBe(true);
  });

  it("invalid: password < 8 karakter", () => {
    const result = registerSchema.safeParse({
      email: "test@example.com",
      password: "short",
      confirmPassword: "short",
      full_name: "Budi",
      identity_number: "123",
    });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0].message).toBe("Password minimal 8 karakter");
  });

  it("invalid: confirmPassword tidak cocok", () => {
    const result = registerSchema.safeParse({
      email: "test@example.com",
      password: "password123",
      confirmPassword: "password999",
      full_name: "Budi",
      identity_number: "123",
    });
    expect(result.success).toBe(false);
    const msg = result.error?.issues.find((i) =>
      i.path.includes("confirmPassword")
    );
    expect(msg?.message).toBe("Password dan konfirmasi tidak cocok");
  });

  it("invalid: full_name kosong", () => {
    const result = registerSchema.safeParse({
      email: "test@example.com",
      password: "password123",
      confirmPassword: "password123",
      full_name: "",
      identity_number: "123",
    });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0].message).toBe("Nama lengkap wajib diisi");
  });

  it("invalid: identity_number kosong", () => {
    const result = registerSchema.safeParse({
      email: "test@example.com",
      password: "password123",
      confirmPassword: "password123",
      full_name: "Budi",
      identity_number: "",
    });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0].message).toBe("Nomor identitas wajib diisi");
  });

  it("TIDAK ada validasi format identity_number (backend tidak punya)", () => {
    const result = registerSchema.safeParse({
      email: "test@example.com",
      password: "password123",
      confirmPassword: "password123",
      full_name: "Budi",
      identity_number: "NIMPALSUSU",
    });
    expect(result.success).toBe(true);
  });
});
