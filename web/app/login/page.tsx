import { Suspense } from "react";
import { LoginForm } from "@/components/auth/LoginForm";

export default function LoginPage() {
  return (
    <div className="flex min-h-[80vh] items-center justify-center px-4">
      <Suspense fallback={<div className="text-muted-foreground">Memuat…</div>}>
        <LoginForm />
      </Suspense>
    </div>
  );
}
