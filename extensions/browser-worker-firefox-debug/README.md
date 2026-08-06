# Yara Browser Worker Debug (Firefox)

Debug version of the browser worker extension for Firefox. **No authentication required**. Designed for development and testing with Cloudflare-protected sites.

## Browser support

| Browser | Extension directory |
|---------|-------------------|
| Chrome  | `extensions/browser-worker-chrome-debug/` |
| Firefox | `extensions/browser-worker-firefox-debug/` |

## Differences from the main extension

| Feature | Main | Debug |
|---------|------|-------|
| Authentication | Requires user token | No token required |
| WebSocket endpoint | `/ws/browser-worker` | `/ws/browser-worker-debug` |
| Storage (Chrome) | `yara_browser_worker` | `yara_browser_worker_debug` |
| Storage (Firefox) | `yara_browser_worker_firefox` | `yara_browser_worker_firefox_debug` |
| Usage | Production | Development/Testing |

## Maintained functionality

- Full HTTP proxy with cookie handling
- Automatic Cloudflare challenge detection
- Tab management for challenge resolution
- HTML extraction with charset support (GBK, UTF-8)
- Automatic reconnection
- Same WebSocket protocol as main extension

## Installation

### Firefox (temporary add-on)

1. Open Firefox and navigate to `about:debugging`
2. Click "This Firefox"
3. Click "Load Temporary Add-on"
4. Select the `manifest.json` file from `browser-worker-firefox-debug/`
5. The extension will appear with a "DEBUG" badge

> **Note:** Firefox temporary add-ons are lost on browser restart. For persistent installation, use `about:debugging` → "Install Add-on From File" or package as a `.xpi`.

## Usage

1. Start the server: `./bin/translator-server`
2. Click on the debug extension icon
3. Click "Connect" (won't ask for credentials)
4. The server will automatically accept the connection

## Workflow for Cloudflare-protected sites

When you need to scrape a Cloudflare-protected site:

1. The debug extension must be connected
2. The server will send the fetch request
3. If Cloudflare detects the request, the extension will open a tab
4. Resolve the challenge manually in the tab
5. The extension will extract the HTML and return it to the server
6. The tab will be closed automatically

## Notes

- This extension is for development only
- Don't use in production
- Storage is separate from main extension (no conflict)
- Both Chrome and Firefox extensions can be installed simultaneously
- Firefox uses separate storage keys (`yara_browser_worker_firefox_debug`) to avoid conflicts with Chrome
