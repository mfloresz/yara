# Yara Browser Worker (Firefox)

Firefox extension (Manifest V3) that proxies HTTP requests through a real browser to bypass Cloudflare. Requires user authentication via `/api/worker-auth/`.

## Installation

1. Open Firefox and navigate to `about:debugging`
2. Click "This Firefox"
3. Click "Load Temporary Add-on"
4. Select the `manifest.json` file from `browser-worker-firefox/`
5. The extension will appear in the toolbar

> **Note:** Temporary add-ons are removed when Firefox restarts. Persistent installation requires a signed `.xpi` from Firefox Add-ons, or a development Firefox profile configured to allow unsigned extensions.

## Usage

1. Start the server: `./bin/translator-server`
2. Click on the extension icon
3. Enter the server address (e.g. `localhost:5176`)
4. Click "Authenticate with the Server" and follow the auth flow
5. The extension connects to the server via WebSocket and proxies browser requests

## Differences from Chrome version

| Aspect | Chrome | Firefox |
|--------|--------|---------|
| Manifest | `extensions/browser-worker-chrome/` | `extensions/browser-worker-firefox/` |
| Storage key | `yara_browser_worker` | `yara_browser_worker_firefox` |
| Extension ID | Chrome-generated | `{720e4198-2455-4bf5-b973-378a8368a6a4}` |
| Install method | Load unpacked in `chrome://extensions` | Temporary add-on in `about:debugging` |

## Features

- Full HTTP proxy with cookie handling
- Automatic Cloudflare challenge detection
- Tab management for challenge resolution
- HTML extraction with charset support (GBK, UTF-8)
- Automatic reconnection
- Same WebSocket protocol as Chrome version
