"use client";

import { useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { registerSchema } from "@/lib/auth/schemas";
import { ApiError } from "@/lib/api/errors";
import { apiFetch } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export function RegisterForm() {
  const router = useRouter();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [fullName, setFullName] = useState("");
  const [identityNumber, setIdentityNumber] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setServerError(null);
    setErrors({});

    const parsed = registerSchema.safeParse({
      email,
      password,
      confirmPassword,
      full_name: fullName,
      identity_number: identityNumber,
    });
    if (!parsed.success) {
      const fieldErrors: Record<string, string> = {};
      for (const err of parsed.error.issues) {
        const key = String(err.path[0]);
        if (!fieldErrors[key]) {
          fieldErrors[key] = err.message;
        }
      }
      setErrors(fieldErrors);
      return;
    }

    setLoading(true);
    try {
      // registerRequest tidak punya confirmPassword dan tidak punya role.
      const body = {
        email,
        password,
        full_name: fullName.trim(),
        identity_number: identityNumber,
      };
      await apiFetch("/auth/register", {
        method: "POST",
        body,
      });
      // Register TIDAK auto-login — redirect ke /login.
      router.push("/login?registered=1");
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.is("EMAIL_TAKEN")) {
          setErrors({ email: "Email sudah terdaftar" });
        } else {
          setServerError(err.message);
        }
      } else {
        setServerError("Gagal mendaftar, coba lagi.");
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card className="w-full max-w-sm">
      <CardHeader>
        <CardTitle className="text-xl">Daftar</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="full_name">Nama lengkap</Label>
            <Input
              id="full_name"
              type="text"
              autoComplete="name"
              value={fullName}
              onChange={(e) => setFullName(e.target.value)}
              error={!!errors.full_name}
              disabled={loading}
            />
            {errors.full_name && (
              <p className="text-xs text-destructive">{errors.full_name}</p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="identity_number">Nomor identitas (NIM/NIP)</Label>
            <Input
              id="identity_number"
              type="text"
              autoComplete="off"
              value={identityNumber}
              onChange={(e) => setIdentityNumber(e.target.value)}
              error={!!errors.identity_number}
              disabled={loading}
            />
            {errors.identity_number && (
              <p className="text-xs text-destructive">{errors.identity_number}</p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="email">Email</Label>
            <Input
              id="email"
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              error={!!errors.email}
              disabled={loading}
            />
            {errors.email && (
              <p className="text-xs text-destructive">{errors.email}</p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              error={!!errors.password}
              disabled={loading}
            />
            {errors.password && (
              <p className="text-xs text-destructive">{errors.password}</p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="confirmPassword">Konfirmasi password</Label>
            <Input
              id="confirmPassword"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              error={!!errors.confirmPassword}
              disabled={loading}
            />
            {errors.confirmPassword && (
              <p className="text-xs text-destructive">
                {errors.confirmPassword}
              </p>
            )}
          </div>

          {serverError && (
            <p className="text-sm text-destructive">{serverError}</p>
          )}

          <Button type="submit" disabled={loading} className="w-full">
            {loading ? "Memuat…" : "Daftar"}
          </Button>
        </form>
        <p className="mt-4 text-center text-sm text-muted-foreground">
          Sudah punya akun?{" "}
          <Link href="/login" className="underline underline-offset-4">
            Masuk
          </Link>
        </p>
      </CardContent>
    </Card>
  );
}
