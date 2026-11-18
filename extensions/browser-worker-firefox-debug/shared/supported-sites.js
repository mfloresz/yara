// Supported novel sites, hardcoded so no backend changes are needed.
// Keep in sync with the Go parser catalog in internal/noveldownloader
// (NewDownloader/NewDownloaderWithClient). Bare domains; matching allows
// any subdomain (e.g. www., m.) via isSupportedUrl.
export const SUPPORTED_SITE_HOSTS = [
  'novelfire.net',
  'novelphoenix.com',
  'fenrirealm.com',
  'floraegarden.com',
  'flyonthewalls.blog',
  'brightnovels.com',
  'cherrymist.cafe',
  'foxaholic.com',
  'empirenovel.com',
  '69shuba.com',
  'skynovels.net',
  'skydemonorder.com',
  'literotica.com',
  'novelarrow.com',
  'wattpad.com',
  'inkitt.com',
  'gaydemon.com',
  'chrysanthemumgarden.com',
  'getinkspired.com',
  'asianovel.net',
  'dmxs.org',
  'epub.pub',
  'pearlandreef.com',

  // Comic sites (media: comic parsers).
  'manhwaweb.com',
  'zonatmo.to',
  'samurai.j5z.xyz',
  'onfmangas.com',
  'lector-mangas.lat',
  'toonkotsu.com',
  'manhuaus.com',
  'manhwashot.lat',
  'mhscans.com',
  'manhuahot.com',
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
  const value = String(serverAddr || '').trim();
  const explicit = /^[a-z][a-z0-9+.-]*:\/\//i.test(value);
  const parsed = new URL(explicit ? value : `https://${value}`);
  if (parsed.protocol === 'wss:') parsed.protocol = 'https:';
  if (parsed.protocol === 'ws:') parsed.protocol = 'http:';
  const host = parsed.hostname.toLowerCase().replace(/\.$/, '');
  const loopback = host === 'localhost' || host.endsWith('.localhost') || host === '[::1]' || /^127\./.test(host);
  if (!explicit && loopback) parsed.protocol = 'http:';
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password ||
      parsed.search || parsed.hash || parsed.pathname !== '/') throw new Error('Configura un origen HTTP(S) válido');
  if (parsed.protocol === 'http:' && !loopback) throw new Error('Los servidores remotos requieren HTTPS');
  return parsed.origin;
}
