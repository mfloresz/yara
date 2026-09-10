// Default novel cover served from the frontend bundle (public/no_cover.jpg).
// Novels without a stored cover get coverPath === "" from the backend; every
// view falls back to this shared URL so the browser downloads it once and
// reuses it for all cover-less novels instead of hitting the authenticated
// per-novel cover endpoint N times.
export const DEFAULT_COVER_URL = "/no_cover.jpg";

export function coverSrc(coverPath?: string | null): string {
  return coverPath?.trim() ? coverPath : DEFAULT_COVER_URL;
}

export function onCoverError(event: Event): void {
  const img = event.target as HTMLImageElement | null;
  if (img && !img.src.endsWith(DEFAULT_COVER_URL)) {
    img.src = DEFAULT_COVER_URL;
  }
}
