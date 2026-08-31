import { onBeforeUnmount, ref, watch, type Ref } from "vue";

export function useDescriptionOverflow(source?: Ref<unknown>) {
  const el = ref<HTMLElement | null>(null);
  const expanded = ref(false);
  const overflow = ref(false);

  function check() {
    const node = el.value;
    if (!node) {
      overflow.value = false;
      return;
    }
    if (expanded.value) return;
    overflow.value = node.scrollHeight > node.clientHeight + 1;
  }

  // Check on mount (el goes from null to element) and whenever the content
  // source changes (e.g. the description was edited or another novel loaded).
  watch(el, () => {
    void Promise.resolve().then(check);
  });
  if (source) {
    watch(source, () => {
      void Promise.resolve().then(check);
    });
  }

  let timer: ReturnType<typeof setTimeout> | null = null;
  function debouncedCheck() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(check, 150);
  }

  if (typeof window !== "undefined") {
    window.addEventListener("resize", debouncedCheck);
  }

  onBeforeUnmount(() => {
    if (timer) clearTimeout(timer);
    if (typeof window !== "undefined") {
      window.removeEventListener("resize", debouncedCheck);
    }
  });

  function reset() {
    expanded.value = false;
  }

  return { el, expanded, overflow, check, reset };
}
