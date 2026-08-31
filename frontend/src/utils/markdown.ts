import { marked } from "marked";
import DOMPurify from "dompurify";

marked.setOptions({
  breaks: true,
  gfm: true,
});

/**
 * Post-processes HTML from marked to improve footnote rendering:
 * - Wraps footnote reference links (`<a href="#fnref:...">`) in `<sup>` tags
 *   with brackets so they render as [1], [2], etc. in the reader.
 * - Adds a `footnotes` class to the footnotes `<ol>` so CSS can style the
 *   footnotes section distinctly from the body text.
 */
function enhanceFootnotes(html: string): string {
  // First, unwrap any existing <sup> around footnote references to avoid
  // double-wrapping on repeated passes.
  html = html.replace(
    /<sup>(<a\s+href="#fnref:[^"]*"[^>]*>.*?<\/a>)<\/sup>/g,
    "$1",
  );
  // Wrap footnote reference links in <sup> tags with brackets: [N].
  html = html.replace(
    /<a\s+href="(#[^"]*)"[^>]*>(\d+)<\/a>/g,
    '<sup><a href="$1">[$2]</a></sup>',
  );
  // Add a `footnotes` class to the last <ol> that contains a #fnref:
  // back-link, which is the footnotes list produced by the markdown converter.
  // The negative lookahead ensures we match the last <ol> (no <ol> after it).
  const footnotesOlRe = /<ol>(?![\s\S]*<ol>)([\s\S]*#fnref:[\s\S]*?<\/ol>)$/;
  html = html.replace(footnotesOlRe, '<ol class="footnotes">$1');
  return html;
}

export function markdownToHtml(markdown: string): string {
  if (!markdown) return "";
  const normalized = markdown.replace(/^•••\s*$/gm, "***");
  const rawHtml = marked.parse(normalized) as string;
  const sanitized = DOMPurify.sanitize(rawHtml);
  return enhanceFootnotes(sanitized);
}
