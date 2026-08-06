# Changelog

## [v0.11.1] - 2026-08-05

## Fixes

- Fixed SkyDemonOrder Livewire catalog loading: browser worker now keeps the catalog tab active, uses viewport-based scroll steps, directly fetches the Livewire component snapshot, and waits for the chapter catalog marker before extraction.
- Fixed browser worker WebSocket read limit (32 MB) to accommodate large Livewire catalog responses.
- Fixed Go WebSocket read deadline handling for browser worker connections.

## [v0.11.0] - 2026-08-05

## What's new

- Added the SkyDemonOrder parser for novel downloading, with full Livewire chapter-catalog support.
- Added `fetch_livewire` browser-worker operation to render JavaScript-dependent project pages through the browser.
- Added browser worker URL helpers (`browserWorkerWebSocketURL`, `browserWorkerHTTPURL`) for consistent protocol handling across extensions.
- Improved footnote rendering in the reader: footnote references render as [1], [2], etc., and the footnotes section is styled distinctly from body text.
- Improved markdown processing for the reader page (footnote support in `markdownToHtml`).
- SkyDemonOrder project pages now detect missing chapter catalogs in direct HTTP responses and retry through the browser worker automatically.

## Fixes

- Fixed fallback client to detect SkyDemonOrder 200-but-not-rendered responses and retry through the browser before falling back to chapter-walking.
- Fixed browser worker reconnect logic and URL construction to handle `ws://`, `wss://`, `http://`, and `https://` server addresses correctly.

[Unreleased]: https://github.com/mfloresz/yara/compare/v0.11.1...HEAD
[v0.11.1]: https://github.com/mfloresz/yara/compare/v0.11.0...v0.11.1
[v0.11.0]: https://github.com/mfloresz/yara/compare/v0.10.0...v0.11.0
[v0.10.0]: https://github.com/mfloresz/yara/compare/v0.9.0...v0.10.0
[Previous release]: https://github.com/mfloresz/yara/releases/tag/v0.10.0
