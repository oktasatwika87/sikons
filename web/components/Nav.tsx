"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useAuth } from "@/lib/auth/AuthContext";
import { Button } from "@/components/ui/button";

export function Nav() {
  const { auth, logout } = useAuth();
  const router = useRouter();

  const handleLogout = async () => {
    await logout();
    router.push("/");
    router.refresh();
  };

  return (
    <nav className="border-b border-border">
      <div className="container mx-auto flex h-14 items-center justify-between px-4">
        <Link href="/" className="font-semibold">
          SIKONS
        </Link>

        <div className="flex items-center gap-3">
          <Link href="/dosen" className="text-sm hover:underline">
            Cari Dosen
          </Link>
          {auth.status === "loading" ? (
            <span className="text-sm text-muted-foreground">Memuat…</span>
          ) : auth.status === "authenticated" && auth.user ? (
            <>
              <span className="text-sm text-muted-foreground">
                {auth.user.full_name}
              </span>
              <Link href="/booking-saya" className="text-sm hover:underline">
                Booking Saya
              </Link>
              <Link href="/akun">
                <Button variant="ghost" size="sm">
                  Akun
                </Button>
              </Link>
              <Button
                variant="outline"
                size="sm"
                onClick={handleLogout}
                type="button"
              >
                Keluar
              </Button>
            </>
          ) : (
            <>
              <Link href="/login">
                <Button variant="ghost" size="sm">
                  Masuk
                </Button>
              </Link>
              <Link href="/register">
                <Button variant="outline" size="sm">
                  Daftar
                </Button>
              </Link>
            </>
          )}
        </div>
      </div>
    </nav>
  );
}
