import { Suspense } from "react";
import { LecturerList } from "@/components/lecturer/LecturerList";

/**
 * /dosen — daftar dosen dengan filter + pagination.
 *
 * Suspense membungkus LecturerList karena useSearchParams men-dynamic-kan
 * halaman ini di App Router (lihat pola /login yang sudah ada).
 */
export default function DosenPage() {
  return (
    <Suspense
      fallback={
        <div className="container mx-auto max-w-4xl px-4 py-8 text-muted-foreground">
          Memuat…
        </div>
      }
    >
      <LecturerList />
    </Suspense>
  );
}
