import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Next 16.3 menulis dua file panduan tambahan ke folder web/ setiap kali
  // `next dev` jalan. Dimatikan supaya working tree tetap bersih dan file itu
  // tidak ikut ter-commit tanpa sengaja di mesin lain.
  agentRules: false,
};

export default nextConfig;
