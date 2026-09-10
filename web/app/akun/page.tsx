"use client";

import { useEffect, useState } from "react";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { useApiFetch } from "@/hooks/useApiFetch";
import type { UserResponse } from "@/lib/api/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatJakartaLongDate } from "@/lib/date";

function AkunContent() {
  const apiFetch = useApiFetch();
  const [user, setUser] = useState<UserResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    apiFetch<{ user: UserResponse }>("/auth/me")
      .then(({ user }) => setUser(user))
      .catch((err) => setError(err instanceof Error ? err.message : "Gagal memuat data"))
      .finally(() => setLoading(false));
  }, [apiFetch]);

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <p className="text-muted-foreground text-sm">Memuat…</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex justify-center py-12">
        <p className="text-destructive text-sm">{error}</p>
      </div>
    );
  }

  if (!user) return null;

  const roleLabel: Record<string, string> = {
    student: "Mahasiswa",
    lecturer: "Dosen",
    admin: "Admin",
  };

  return (
    <div className="container mx-auto max-w-lg px-4 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Akun Saya</h1>
      <Card>
        <CardHeader>
          <CardTitle>Data Akun</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3">
          <div className="grid grid-cols-[8rem_1fr] gap-1 text-sm">
            <span className="text-muted-foreground">Nama</span>
            <span>{user.full_name}</span>

            <span className="text-muted-foreground">Email</span>
            <span>{user.email}</span>

            <span className="text-muted-foreground">Nomor identitas</span>
            <span>{user.identity_number ?? "—"}</span>

            <span className="text-muted-foreground">Peran</span>
            <span>{roleLabel[user.role] ?? user.role}</span>

            <span className="text-muted-foreground">Status</span>
            <span>{user.is_active ? "Aktif" : "Dinonaktifkan"}</span>

            <span className="text-muted-foreground">Bergabung</span>
            <span>{formatJakartaLongDate(user.created_at)}</span>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export default function AkunPage() {
  return (
    <RequireAuth>
      <AkunContent />
    </RequireAuth>
  );
}
