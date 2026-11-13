export type ImportedChapter = {
  id: string;
  title: string;
  content: string;
  order: number;
};

function normalizePotentialChapterTitle(line: string): string {
  return line.trim().replace(/^\uFEFF/, "").replace(/^#{1,6}\s+/, "").replace(/\s+#{1,6}\s*$/, "").trim();
}

function extractChapterNumberFromTitle(title: string): number | null {
  const normalized = normalizePotentialChapterTitle(title);
  if (!normalized) return null;

  const chapterNumberRegex = /^(?:cap[ií]tulo|chapter|chap|ch)\b[\s.:_-]*(\d+)\b/i;
  const chapterMatch = chapterNumberRegex.exec(normalized);
  if (chapterMatch) return Number(chapterMatch[1]);

  const numberedTitleRegex = /^(\d+)[.)]\s+(.+)$/;
  const numberedMatch = numberedTitleRegex.exec(normalized);
  if (numberedMatch) return Number(numberedMatch[1]);

  return null;
}

function extractChapterTitleFromLine(line: string): string | null {
  const normalized = normalizePotentialChapterTitle(line);
  if (!normalized) return null;

  const chapterTitleRegex = /^(?:cap[ií]tulo|chapter|chap|ch)\b[\s.:_-]*.+$/i;
  if (chapterTitleRegex.test(normalized)) return normalized;

  const numberedTitleRegex = /^(\d+)[.)]\s+(.+)$/;
  const numberedMatch = numberedTitleRegex.exec(normalized);
  if (numberedMatch) return `${numberedMatch[1]}. ${numberedMatch[2].trim()}`;

  return null;
}

export async function readTxtChapters(file: File): Promise<ImportedChapter[]> {
  const text = await file.text();
  const normalized = text.replace(/\r\n/g, "\n");
  const lines = normalized.split("\n");
  const result: ImportedChapter[] = [];
  let current: { title: string; lines: string[]; detectedOrder: number | null } | null = null;
  let index = 0;

  const pushCurrent = () => {
    if (!current) return;
    index++;
    result.push({
      id: `chapter-${index}`,
      title: current.title || `Capítulo ${index}`,
      content: current.lines.join("\n").trim(),
      order: current.detectedOrder ?? index,
    });
  };

  for (const line of lines) {
    const title = extractChapterTitleFromLine(line);
    if (title) {
      pushCurrent();
      current = { title, lines: [], detectedOrder: extractChapterNumberFromTitle(title) };
      continue;
    }

    if (!current) {
      if (!line.trim()) continue;
      current = { title: "Capítulo 1", lines: [], detectedOrder: 1 };
    }
    current.lines.push(line);
  }
  pushCurrent();

  if (result.length === 0) {
    result.push({
      id: "chapter-1",
      title: file.name.replace(/\.(txt|md)$/i, ""),
      content: normalized.trim(),
      order: 1,
    });
  }

  return [...result].sort((a, b) => a.order - b.order);
}
