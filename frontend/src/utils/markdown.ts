import { marked } from "marked";
import DOMPurify from "dompurify";

marked.setOptions({
  breaks: true,
  gfm: true,
});

export function markdownToHtml(markdown: string): string {
  if (!markdown) return "";
  const normalized = markdown.replace(/^•••\s*$/gm, "***");
  const rawHtml = marked.parse(normalized) as string;
  return DOMPurify.sanitize(rawHtml);
}
