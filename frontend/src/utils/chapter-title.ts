/**
 * Display helper for chapter titles.
 *
 * Source sites frequently bake a redundant "Chapter N :" / "第N章" prefix
 * into the chapter title. The list already shows the position in a dedicated
 * column (`#NN`), so the prefix is noise when scanning. Strip only a leading
 * prefix followed by a number; anything else keeps the original title.
 */
const LATIN_PREFIX =
  /^(?:chapter|chapitre|cap[ií]tulo|cap|ch|episode|episodio|ep)\s*\.?\s*0*\d+[\s\p{P}\p{S}]{0,6}/iu;
const CJK_PREFIX = /^第\s*0*\d+\s*章[\s\p{P}\p{S}]{0,6}/u;

export function displayChapterTitle(raw: string | null | undefined): string {
  const title = (raw ?? "").trim();
  if (!title) return title;
  for (const pattern of [CJK_PREFIX, LATIN_PREFIX]) {
    const match = title.match(pattern);
    if (match) {
      const rest = title.slice(match[0].length).trim();
      if (rest) return rest;
    }
  }
  return title;
}
