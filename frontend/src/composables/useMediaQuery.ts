import { onScopeDispose, ref, type Ref } from "vue";

/** Reactiva window.matchMedia: true mientras la query coincide con el viewport. */
export function useMediaQuery(query: string): Ref<boolean> {
  const mql = window.matchMedia(query);
  const matches = ref(mql.matches) as Ref<boolean>;
  const onChange = (event: MediaQueryListEvent) => {
    matches.value = event.matches;
  };
  mql.addEventListener("change", onChange);
  onScopeDispose(() => mql.removeEventListener("change", onChange));
  return matches;
}
