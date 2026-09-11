"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/lib/auth/AuthContext";
import { Button } from "@/components/ui/button";

/**
 * Redirect ke halaman sesuai role untuk user yang sudah login.
 *
 * Pola: sama dengan RequireAuth — cek auth.status di useEffect,
 * redirect pakai router.replace().
 * User anonim melihat landing page, bukan di-redirect.
 */
export function RoleRedirect() {
  const { auth } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (auth.status !== "authenticated" || !auth.user) return;

    switch (auth.user.role) {
      case "lecturer":
        router.replace("/dashboard");
        break;
      case "student":
        router.replace("/dosen");
        break;
      case "admin":
        router.replace("/akun");
        break;
    }
  }, [auth.status, auth.user, router]);

  // Tampilkan loading singkat selagi redirect
  return (
    <div className="flex flex-1 items-center justify-center">
      <p className="text-muted-foreground">Memuat…</p>
    </div>
  );
}

/**
 * Landing page untuk user anonim (belum login).
 */
export function LandingPage() {
  return (
    <div className="flex flex-1 items-center justify-center px-4">
      <div className="text-center">
        <h1 className="text-3xl font-bold tracking-tight">SIKONS</h1>
        <p className="mt-2 text-muted-foreground">
          Sistem Reservasi Konsultasi Dosen
        </p>
        <p className="mt-1 text-sm text-muted-foreground">
          Masuk atau daftar untuk memulai reservasi.
        </p>
        <div className="mt-6 flex justify-center gap-3">
          <Link href="/login">
            <Button variant="outline" size="sm">
              Masuk
            </Button>
          </Link>
          <Link href="/register">
            <Button size="sm">Daftar</Button>
          </Link>
        </div>
      </div>
    </div>
  );
}
