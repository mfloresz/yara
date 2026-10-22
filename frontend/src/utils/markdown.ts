import { marked } from "marked";
import DOMPurify from "dompurify";
import type { ChapterImage } from "../domain";

marked.setOptions({
  breaks: true,
  gfm: true,
});

const IMAGE_TOKEN_RE = /\[\[IMG-\d+\]\]/g;

function escapeHtmlAttribute(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/"/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

/**
 * Replaces [[IMG-n]] inline image tokens with <img> tags pointing at the
 * authenticated image URLs before markdown parsing. Tokens without a
 * matching image are dropped. Raw <img> HTML passes through marked and
 * DOMPurify's default allow-list untouched.
 */
function resolveImageTokens(markdown: string, images?: ChapterImage[]): string {
  if (!images?.length || !markdown.includes("[[IMG-")) return markdown;
  const byToken = new Map(images.map((img) => [img.token, img]));
  return markdown.replace(IMAGE_TOKEN_RE, (token) => {
    const img = byToken.get(token);
    if (!img?.url) return "";
    const alt = img.alt ? escapeHtmlAttribute(img.alt) : "";
    return `<img src="${escapeHtmlAttribute(img.url)}" alt="${alt}" loading="lazy"/>`;
  });
}

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

export function markdownToHtml(markdown: string, images?: ChapterImage[]): string {
  if (!markdown) return "";
  const normalized = resolveImageTokens(markdown.replace(/^•••\s*$/gm, "***"), images);
  const rawHtml = marked.parse(normalized) as string;
  // The reader renders untrusted stored content: source-site text and
  // descriptions (imported verbatim), member-authored chapters of public
  // novels, and EPUB text. DOMPurify's default allow-list keeps
  // <style>/<form>/<input>/<button> and never inspects style attribute
  // values; combined with the app CSP (style-src 'unsafe-inline',
  // form-action 'self') and the cookie-authenticated API (no CSRF token),
  // any surviving injection becomes persistent UI redressing plus
  // one-click same-origin session riding (SameSite=Strict does not apply
  // to forms injected into the app's own origin — the server also rejects
  // Sec-Fetch-Mode: navigate writes). Restrict to content-only markup: no
  // style elements/attributes, no form controls or form-aiming
  // attributes. Task-list checkboxes (marked renders
  // <input type=checkbox disabled>) stay inert without a <form>.
  const sanitized = DOMPurify.sanitize(rawHtml, {
    FORBID_TAGS: ["style", "form"],
    FORBID_ATTR: ["style", "action", "method", "formaction", "enctype"],
  });
  return enhanceFootnotes(sanitized);
}
