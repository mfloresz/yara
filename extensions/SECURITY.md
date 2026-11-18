# Browser worker security

These rules apply to Chrome and Firefox, including the debug workers.

- **Server transport:** remote server addresses without a scheme use HTTPS/WSS. Explicit remote HTTP/WS is rejected. HTTP/WS remains available for loopback development (`localhost`, `*.localhost`, `127.0.0.0/8`, `::1`). A LAN server therefore needs HTTPS, not a plaintext LAN exception.
- **Authorization:** start from the extension popup. The background opens a dedicated tab and stores a 256-bit cryptographic `client_state`, exact server origin and tab ID for five minutes. Only that callback can replace credentials; it is consumed before credentials are written, including under duplicate callback events. The obsolete extension callback page cannot write credentials and is not web-accessible. Backend support for `client_state` is required.
- **Fetch redirects:** background requests use manual redirects. A visible `Location` is validated before the next request. An opaque redirect cannot be inspected and instead requires the guarded browser-tab route; no automatic-fetch redirect is permitted.
- **Browser-tab fallback:** the tab starts at `about:blank`. Session-scoped declarative network rules must be installed successfully before navigation. They block non-HTTP(S) network requests, local hostnames (`localhost`, `.localhost`, `.local`, `.internal`) and all IP literals, conservatively including public IP literals. This protects redirected navigation and subrequests while keeping domain-based sites, challenge pages and their public CDNs usable. Browsers without the necessary API/permission fail closed rather than navigating unguarded. Reload the extension after updating its permissions.
- **Boundary:** DNS hostnames remain supported for existing split-horizon use. These URL-level controls do not resolve or pin DNS addresses, and do not claim protection from DNS rebinding or a public-looking hostname resolving to private IP space. Guarded-tab fallback does not support IP-literal destinations. Real Chrome/Firefox extension loading and challenge UX require a browser integration check; mocked tests alone do not prove browser API enforcement.

## Focused regression tests

Run from the repository root:

```sh
node --test extensions/browser-worker-security.test.cjs
```

To reproduce the original transport, unsolicited-callback and pre-access redirect failures against the current committed source while keeping the test harness:

```sh
YARA_EXTENSION_BASELINE=1 node --test --test-name-pattern='remote addresses|private initial fetch|private redirect hop|unsolicited callback' extensions/browser-worker-security.test.cjs
```

The baseline switch reads `HEAD` via `git show`; once the security fix is committed, that baseline is no longer expected to fail.
