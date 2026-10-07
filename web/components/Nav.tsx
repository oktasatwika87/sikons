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
          {auth.status === "loading" ? (
            <span className="text-sm text-muted-foreground">Memuat…</span>
          ) : auth.status === "authenticated" && auth.user ? (
            <>
              <span className="text-sm text-muted-foreground">
                {auth.user.full_name}
              </span>
              {/* Link fitur per role */}
              {auth.user.role === "student" && (
                <Link href="/dosen" className="text-sm hover:underline">
                  Cari Dosen
                </Link>
              )}
              {auth.user.role === "lecturer" && (
                <>
                  <Link href="/dashboard" className="text-sm hover:underline">
                    Dashboard
                  </Link>
                  <Link href="/ketersediaan" className="text-sm hover:underline">
                    Ketersediaan
                  </Link>
                </>
              )}
              <Link href="/konsultasi" className="text-sm hover:underline">
                Konsultasi
              </Link>
              <Link href="/akun" className="text-sm hover:underline">
                Akun
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
              <Button variant="ghost" size="sm" render={<Link href="/login" />}>
                Masuk
              </Button>
              <Button variant="outline" size="sm" render={<Link href="/register" />}>
                Daftar
              </Button>
            </>
          )}
        </div>
      </div>
    </nav>
  );
}
