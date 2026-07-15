# Yara Browser Worker (Debug)

Debug version of the browser worker extension. **No authentication required**. Designed for development and testing with Cloudflare-protected sites.

## Differences from the main extension

| Feature | Main | Debug |
|---------|------|-------|
| Authentication | Requires user token | No token required |
| WebSocket endpoint | `/ws/browser-worker` | `/ws/browser-worker-debug` |
| Storage | `yara_browser_worker` | `yara_browser_worker_debug` |
| Usage | Production | Development/Testing |

## Maintained functionality

- Full HTTP proxy with cookie handling
- Automatic Cloudflare challenge detection
- Tab management for challenge resolution
- HTML extraction with charset support (GBK, UTF-8)
- Automatic reconnection
- Same WebSocket protocol as main extension

## Installation

1. Open Chrome and navigate to `chrome://extensions/`
2. Enable "Developer mode" (top right corner)
3. Click "Load unpacked"
4. Select the `browser-worker-debug/` folder
5. The extension will appear with a "DEBUG" badge

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
- Both extensions can be installed simultaneously
