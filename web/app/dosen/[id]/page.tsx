import { Suspense } from "react";
import { LecturerDetail } from "@/components/lecturer/LecturerDetail";

/**
 * /dosen/[id] — detail dosen + kalender slot mingguan (read-only).
 *
 * [id] adalah UUID dosen. Validasi format dilakukan di sisi server dengan
 * Postgres (invalid UUID → 404 NotFound).
 */
export default async function DosenDetailPage({
  params,
}: PageProps<"/dosen/[id]">) {
  const { id } = await params;
  return (
    <Suspense
      fallback={
        <div className="container mx-auto max-w-5xl px-4 py-8 text-muted-foreground">
          Memuat…
        </div>
      }
    >
      <LecturerDetail lecturerId={id} />
    </Suspense>
  );
}
