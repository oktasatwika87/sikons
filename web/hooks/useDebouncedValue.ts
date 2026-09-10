"use client";

import { useEffect, useState } from "react";

/**
 * Tunda pembaruan nilai selama `delay` milidetik.
 *
 * Dipakai untuk input "q" di halaman dosen — tiap ketukan keyboard jangan
 * langsung trigger query baru, tunggu ~350ms supaya responsif tapi tidak
 * spam request. Backend ILIKE `%q%` tidak perlu dipanggil tiap karakter.
 *
 * Implementasi sengaja kecil (tidak pakai library debounce) — lihat
 * keputusan M5c #3.
 */
export function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);

  return debounced;
}
