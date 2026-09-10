"use client";

import { useRouter, usePathname } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { useAuth } from "@/lib/auth/AuthContext";

interface RequireAuthProps {
  children: ReactNode;
}

/**
 * Proteksi route client-side.
 *
 * - status "loading": render children dengan loading indicator.
 * - status "anonymous": redirect ke /login?next=<path-sekarang>.
 * - status "authenticated": render children.
 *
 * Tidak ada middleware Next.js — murni client-side.
 */
export function RequireAuth({ children }: RequireAuthProps) {
  const { auth } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (auth.status === "anonymous") {
      const next = encodeURIComponent(pathname);
      router.replace(`/login?next=${next}`);
    }
  }, [auth.status, pathname, router]);

  if (auth.status === "loading" || auth.status === "anonymous") {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <p className="text-muted-foreground text-sm">Memuat…</p>
      </div>
    );
  }

  return <>{children}</>;
}
