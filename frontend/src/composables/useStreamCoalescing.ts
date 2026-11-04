import { onBeforeUnmount } from "vue";

// Coalesces high-frequency stream events (text/reasoning deltas) to one DOM
// mutation per animation frame. Without it every token would patch the
// transcript; with it the live tail re-renders at display refresh.
export function useStreamCoalescing() {
  let raf: number | null = null;
  let dirty = false;
  let pending: (() => void) | null = null;

  function schedule(apply: () => void): void {
    pending = apply;
    dirty = true;
    if (raf !== null) return;
    raf = requestAnimationFrame(() => {
      raf = null;
      if (!dirty) return;
      dirty = false;
      pending?.();
      pending = null;
    });
  }

  function flushNow(apply: () => void): void {
    if (raf !== null) {
      cancelAnimationFrame(raf);
      raf = null;
    }
    dirty = false;
    pending = null;
    apply();
  }

  function cancel(): void {
    if (raf !== null) {
      cancelAnimationFrame(raf);
      raf = null;
    }
    dirty = false;
    pending = null;
  }

  function dispose(): void {
    cancel();
  }

  onBeforeUnmount(dispose);

  return { schedule, flushNow, cancel, dispose };
}
