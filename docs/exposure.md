# Exposing Yara to the internet

Guide for deploying Yara as a multi-user, invitation-only service. The
supported exposure path is **Cloudflare Tunnel** (TLS terminates at
Cloudflare's edge; Yara keeps serving plain HTTP on a private interface).

## What the server already does

These protections are built in — no reverse-proxy configuration needed:

- `/_/` (PocketBase superuser dashboard) answers **404**; superuser API access
  is IP-restricted to loopback at startup.
- Registration requires an invitation once the first (admin) user exists.
- Rate limiting: register/login 5/min per IP, invitation validate/accept
  10/min per IP (429 + `Retry-After: 60`).
- Auth endpoints cap request bodies at 16 KB.
- Security headers on every response: CSP (`script-src 'self'`),
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
  `Permissions-Policy`, and HSTS **when the request arrives over HTTPS**.
- Session cookies are `HttpOnly`, `SameSite=Strict`, and `Secure` whenever the
  request arrives via `X-Forwarded-Proto: https` (cloudflared always sets it).
- Provider API keys are encrypted at rest (AES-256-GCM).

## Deployment checklist (Cloudflare Tunnel)

1. **Set `APP_ENCRYPTION_KEY` explicitly** (base64 or hex, exactly 32 bytes
   decoded). If you rely on the auto-generated `<data-dir>/app.key`, you must
   back that file up — losing it makes every stored API key undecryptable.

   ```bash
   openssl rand -base64 32
   export APP_ENCRYPTION_KEY="<generated key>"
   ```

2. **Create the admin account.** On a fresh install, open the app once and
   register through the setup screen — the first account becomes the admin.
   On a pre-existing install promote an existing user instead:

   ```bash
   ./translator-server -promote-admin you@example.com
   ```

3. **Run the server on localhost only** so it is reachable exclusively
   through the tunnel:

   ```bash
   ./translator-server --addr 127.0.0.1:5176 --data-dir "$HOME/data"
   ```

4. **Create the tunnel** (cloudflared installed and authenticated):

   ```bash
   cloudflared tunnel create yara
   cloudflared tunnel route dns yara yara.example.com
   ```

   `~/.cloudflared/config.yml`:

   ```yaml
   tunnel: yara
   credentials-file: /home/<user>/.cloudflared/tunnels/<tunnel-id>.json
   ingress:
     - hostname: yara.example.com
       service: http://127.0.0.1:5176
       originRequest:
         noTLSVerify: true
     - service: http_status:404
   ```

   ```bash
   cloudflared tunnel run yara   # or install as a systemd service
   ```

5. **Invite users** from the Yara admin panel (`/admin` → Invitaciones). The
   invitation URL is shown once; share it over a private channel. Invitations
   expire after 7 days and are single-use.

6. **Backups.** `data/` contains the SQLite database (users, novels,
   encrypted keys) and uploaded files. Back it up before upgrades; the
   browser-worker extension tokens and reading progress live there too.

## TLS via reverse proxy instead of a tunnel

If you use a VPS with an independent reverse proxy (Caddy, nginx) instead of
Cloudflare Tunnel, the same rules apply: terminate TLS at the proxy, forward
to `127.0.0.1:5176`, and make sure the proxy sets `X-Forwarded-Proto: https`
so the `Secure` cookie flag and HSTS activate. Caddy does this automatically.

## Accepted risks (deliberate, documented)

- Logout clears the cookie but the PB token remains valid until its natural
  30-day expiry (it is HttpOnly + SameSite=Strict; rotate by changing the
  password once a password-change feature exists).
- No per-account lockout or CAPTCHA; the per-IP login rate limit is the
  brute-force defense.
- Audit logging goes to stdout as structured slog lines (no persistent audit
  collection); ship them if you need retention.
