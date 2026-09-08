// Supported novel sites, hardcoded so no backend changes are needed.
// Keep in sync with the Go parser catalog in internal/noveldownloader
// (NewDownloader/NewDownloaderWithClient). Bare domains; matching allows
// any subdomain (e.g. www., m.) via isSupportedUrl.
export const SUPPORTED_SITE_HOSTS = [
  'novelfire.net',
  'novelphoenix.com',
  'fenrirealm.com',
  'floraegarden.com',
  'cherrymist.cafe',
  'empirenovel.com',
  '69shuba.com',
  'skynovels.net',
  'skydemonorder.com',
  'literotica.com',
  'wtr-lab.com',
  'novelarrow.com',
  'wattpad.com',
  'inkitt.com',
];

// Match patterns for chrome.contextMenus documentUrlPatterns. Both the bare
// host and any subdomain are listed because "*://*.host/*" alone does not
// reliably match the bare host across Chrome/Firefox.
export const SUPPORTED_SITE_PATTERNS = SUPPORTED_SITE_HOSTS.flatMap((host) => [
  `*://${host}/*`,
  `*://*.${host}/*`,
]);

export function isSupportedUrl(rawUrl) {
  try {
    const host = new URL(rawUrl).hostname.toLowerCase();
    return SUPPORTED_SITE_HOSTS.some((h) => host === h || host.endsWith(`.${h}`));
  } catch {
    return false;
  }
}

// Normalizes the configured serverAddr (host:port or full origin) to an
// http(s) origin used to open the Yara tab.
export function yaraBaseUrl(serverAddr) {
  const value = String(serverAddr || '').trim().replace(/\/$/, '');
  if (/^https?:\/\//i.test(value)) return value;
  return `http://${value}`;
}
