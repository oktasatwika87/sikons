/**
 * Test useDebouncedValue — verifikasi delay pembaruan nilai pakai fake timers.
 */
import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";

describe("useDebouncedValue", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("nilai awal sama dengan value awal; baru berubah setelah delay", () => {
    const { result, rerender } = renderHook(
      ({ value }) => useDebouncedValue(value, 350),
      { initialProps: { value: "awal" } }
    );

    expect(result.current).toBe("awal");

    rerender({ value: "ubah" });
    // Sebelum delay lewat, nilai masih "awal".
    expect(result.current).toBe("awal");

    act(() => {
      vi.advanceTimersByTime(349);
    });
    expect(result.current).toBe("awal");

    act(() => {
      vi.advanceTimersByTime(1);
    });
    // Tepat di ms ke-350 nilai diperbarui.
    expect(result.current).toBe("ubah");
  });

  it("perubahan berturut-turut dalam delay direset ke perubahan TERAKHIR", () => {
    const { result, rerender } = renderHook(
      ({ value }) => useDebouncedValue(value, 350),
      { initialProps: { value: "a" } }
    );

    rerender({ value: "b" });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender({ value: "c" });
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender({ value: "d" });

    // Total baru 200ms sejak perubahan pertama — belum boleh commit.
    expect(result.current).toBe("a");

    act(() => {
      vi.advanceTimersByTime(350);
    });
    // Setelah delay penuh dari perubahan TERAKHIR, hasilnya "d" — bukan "b".
    expect(result.current).toBe("d");
  });
});
