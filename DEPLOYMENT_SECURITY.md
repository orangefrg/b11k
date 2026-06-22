# B11K Secure Exposure Plan

This app should be exposed as two public surfaces that route to the same Go
backend:

- Web UI: `https://b11k.example.com`
- Native API: `https://api.b11k.example.com`

The web UI can stay behind Cloudflare Access SSO. The native API must remain
reachable by iOS without browser SSO and must rely on B11K bearer session
tokens.

Current web auth is intentionally single-user/self-hosted. Do not expose the web
UI without Cloudflare Access or an equivalent gate unless web sessions are
refactored to be per-user. The native API is the only public unauthenticated
surface, and every non-auth mobile endpoint must require a B11K bearer session
token.

## What Is Ready On Localhost/LAN

- `B11K_PUBLIC_API_HOST` / `public_api_host` lets the backend know which host is
  the native API host.
- `B11K_WEB_HOST` / `web_host` lets the backend reject unknown public Host
  headers when a production web host is configured.
- Requests to the configured API host are limited to `/api/mobile/*`.
- Requests to other public hosts are not allowed to use `/api/mobile/*`.
- Localhost and private LAN hosts are exempt so iPhone development still works.
- Docker defaults no longer force the rejected `b11k://strava-callback`
  redirect URI.
- Public HTTPS deployments reject non-HTTPS forwarded requests.
- Forwarded host, protocol, and client IP headers are trusted only when the
  immediate peer is localhost or a private network proxy, such as Cloudflare
  Tunnel forwarding to `localhost`.
- Mobile API responses are marked `Cache-Control: no-store`.
- Browser-origin requests to `/api/mobile/*` are rejected, except the Strava
  callback endpoint.
- Public mobile and Strava auth endpoints have in-process per-IP rate limits.
- B11K mobile session tokens are stored as SHA-256 storage keys instead of raw
  bearer tokens.
- Strava access and refresh tokens can be encrypted at rest with
  `B11K_TOKEN_ENCRYPTION_KEY`.
- B11K mobile sessions expire after 90 days and Strava access tokens continue
  to refresh using the stored Strava refresh token.

## Local LAN Settings

For current iPhone development:

```env
B11K_WEB_HOST=localhost
B11K_PUBLIC_API_HOST=
B11K_WEB_PROTOCOL=http
B11K_IOS_REDIRECT_URI=http://<your-lan-ip>:8080/api/mobile/auth/callback
```

In Strava settings, use callback domain:

```text
<your-lan-ip>
```

## Production Settings

When moving to a VPS or stable host:

```env
B11K_WEB_HOST=b11k.example.com
B11K_PUBLIC_API_HOST=api.b11k.example.com
B11K_WEB_PROTOCOL=https
B11K_STRAVA_REDIRECT_URI=https://b11k.example.com/strava/callback
B11K_IOS_REDIRECT_URI=https://api.b11k.example.com/api/mobile/auth/callback
B11K_TOKEN_ENCRYPTION_KEY=<32-byte base64 key>
```

In Strava settings, use callback domain:

```text
api.b11k.example.com
```

If web login should also remain available on the same Strava app, use a common
parent callback domain only if Strava accepts it for both hosts. Otherwise, use
one public host and split paths, or use separate Strava apps for web and iOS.

Generate the token encryption key on the host where you manage secrets:

```sh
openssl rand -base64 32
```

Keep this value in `.env` and back it up separately from the database. Existing
plaintext token rows remain readable after the key is added and are rewritten as
encrypted values the next time the mobile session is loaded or refreshed. Do not
rotate the key casually: values encrypted with the old key cannot be decrypted
unless key-rotation support is added first.

## Cloudflare Tunnel Shape

Route both hostnames to the same local backend:

```yaml
ingress:
  - hostname: b11k.example.com
    service: http://localhost:8080

  - hostname: api.b11k.example.com
    service: http://localhost:8080

  - service: http_status:404
```

Cloudflare Access:

- Protect `b11k.example.com`.
- Do not protect `api.b11k.example.com` with browser SSO.
- Do not put Cloudflare Access service-token secrets into the iOS app.
- If Strava requires the iOS callback to use the protected web host, bypass
  only `https://b11k.example.com/api/mobile/auth/callback`. Do not bypass all
  `/api/mobile/*` paths on the web host.

## Stop Point

Stay on localhost/home LAN until:

- iOS auth works through the LAN HTTP callback.
- Sync and activity browsing work after app reinstall.
- `B11K_PUBLIC_API_HOST` host separation is tested with local `Host` headers.

After that, switch to a VPS or always-on host and configure Cloudflare Tunnel.

## Production Smoke Tests

After deploying, check:

```sh
curl -i https://api.b11k.example.com/
curl -i https://api.b11k.example.com/api/mobile/auth/start
curl -i https://b11k.example.com/api/mobile/auth/callback
curl -i https://b11k.example.com/api/mobile/auth/session
```

Expected:

- API root returns 404.
- API auth start returns 200 JSON.
- Web-host mobile callback reaches B11K and returns `400 missing state` when
  called manually.
- Web-host mobile auth session is blocked by Cloudflare Access or backend 404.
