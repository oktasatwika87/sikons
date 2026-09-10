"use client";

import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/lib/auth/AuthContext";
import { Nav } from "@/components/Nav";

/**
 * Wrapper client untuk seluruh halaman.
 *
 * Menyatukan tiga hal yang harus ada di root tree:
 *  1. QueryClientProvider — TanStack Query untuk fetching/caching (M5c).
 *     QueryClient dibuat via useState(() => new QueryClient()) — BUKAN
 *     module scope — supaya tiap render server tidak berbagi state dengan
 *     request lain. Ini jebakan klasik Next.js App Router.
 *  2. AuthProvider — login/logon/refresh (M5b).
 *  3. Nav — header global.
 *
 * Nama "Providers" sengaja generik (bukan AuthLayout) karena tanggung
 * jawabnya sudah melampaui auth — lihat RINGKASAN-M5C keputusan #1.
 */
export default function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // Retry 1x pada error jaringan — endpoint publik (M5c) tidak
            // pernah kena retry TOKEN_EXPIRED karena tidak auth-required.
            retry: 1,
            refetchOnWindowFocus: false,
          },
        },
      })
  );

  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Nav />
        <main className="flex-1">{children}</main>
      </AuthProvider>
    </QueryClientProvider>
  );
}
