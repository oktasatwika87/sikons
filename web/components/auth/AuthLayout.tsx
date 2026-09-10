"use client";

import { AuthProvider } from "@/lib/auth/AuthContext";
import { Nav } from "@/components/Nav";
import type { ReactNode } from "react";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <AuthProvider>
      <Nav />
      <main className="flex-1">{children}</main>
    </AuthProvider>
  );
}
