"use client";

import { useAuth } from "@/lib/auth/AuthContext";
import { RoleRedirect, LandingPage } from "@/components/auth/RoleRedirect";

/**
 * Landing page — redirect sesuai role untuk user yang sudah login.
 *
 * User anonim melihat halaman landing standar.
 */
export default function Home() {
  const { auth } = useAuth();

  if (auth.status === "authenticated") {
    return <RoleRedirect />;
  }

  // loading atau anonymous → landing page
  return <LandingPage />;
}
