# B11K Security Audit Checklist

Use this checklist to guide manual review after automated scans. Validate each item against current source, config, and deployment assumptions.

## Residue And Secrets

- Check tracked files for real IPs, domains, emails, names, Apple team IDs, bundle IDs, database hosts, Strava app IDs, and copied local paths.
- Check ignored files only by key name and risk; do not print values from `.env`, credentials, tokens, database URLs, or private domains.
- Confirm `.env`, `.env.*`, `config.yaml`, Xcode `xcuserdata`, archives, IPA exports, derived data, scanner reports, and logs are not tracked.
- Run a redacted secret scanner on git history and working files before publishing.
- If any live secret appears in terminal output, reports, chat, or logs, recommend rotation before public release.

## iOS App

Storage:

- Session tokens must live in Keychain, not UserDefaults. Legacy migration should remove old plaintext values.
- Keychain accessibility should match the app threat model. For this single-user app, `ThisDeviceOnly` is preferred for bearer session tokens.
- Avoid storing Strava tokens, activity coordinates, maps, or profile data in plaintext caches unless explicitly intended.
- Check logs, debug labels, crash output, and error views for tokens, OAuth codes, precise locations, private URLs, or raw server responses.

Network:

- Production builds should reject plain HTTP except localhost/private LAN debug use.
- `NSAllowsArbitraryLoads` should not be enabled globally for release.
- Backend URL entry should validate scheme and host before sending Authorization headers.
- Decide whether certificate pinning is worth the operational cost. For a personal/small app it can be optional, but document the decision.
- Confirm OAuth callback URLs and custom schemes cannot be abused to inject tokens or confuse state handling.

Auth and platform:

- Confirm mobile session token is generated server-side, high entropy, revocable/expiring, and stored only in Keychain on the app.
- Confirm auth state/OAuth callback handling rejects missing, expired, replayed, or mismatched state.
- Confirm settings/profile screens do not expose secret material.
- Check URL scheme, bundle ID, team ID, and app group/entitlements before publishing.
- Consider background snapshot/privacy behavior for screens that show precise route maps or profile data.

Code quality:

- Review all JSON decoding, date parsing, URL construction, and map metadata handling for graceful failure on malformed server data.
- Review concurrency/state transitions around sign-in, sign-out, token refresh, and backend URL changes.
- Ensure release builds do not include debug-only screens or development-only bypasses.

## Go Backend And Mobile API

Authentication:

- Every `/api/mobile/*` endpoint except the OAuth callback/exchange path must require a valid bearer mobile session.
- Session lookup should use a hashed storage key, not raw bearer tokens.
- Session expiry, revocation, sign-out, and Strava refresh failure behavior should be explicit.
- Error responses should not reveal access tokens, refresh tokens, token encryption keys, SQL details, or internal topology.

Authorization and scoping:

- All activity, segment, discovered, profile, and match queries must be scoped to the authenticated athlete.
- Caches that contain activity-derived data should include athlete scoping if the deployment can ever become multi-user.
- Confirm segment matching and discovered-area endpoints cannot leak another athlete's route, segment, activity ID, or coordinate data.

Transport and public exposure:

- Production config should force HTTPS, set the public API host, and reject unexpected Host/Origin combinations.
- Mobile API responses should use no-store cache headers.
- Browser origins should be rejected for bearer API endpoints unless explicitly intended.
- Rate limits should cover auth callback/exchange, mobile API reads, and expensive segment/discovered computations.
- Public site routes and mobile API routes should have clearly separated host/path behavior.

Token and data protection:

- Strava access/refresh tokens should be encrypted at rest in production. Fail closed if a public deployment lacks `B11K_TOKEN_ENCRYPTION_KEY`.
- Encryption keys should be generated with adequate entropy, never logged, and rotated with a documented plan.
- Database backups and logs should be treated as sensitive because they may contain routes, coordinates, athlete IDs, and encrypted tokens.

Input handling:

- Validate numeric query parameters such as tolerance, segment ID, paging, dates, and coordinate bounds.
- Bound expensive geospatial operations by limit, timeout, tolerance range, and authenticated athlete.
- Prefer parameterized SQL and typed query builders over string concatenation.
- Review file, archive, template, command execution, redirect, SSRF, and path traversal surfaces even if currently absent.

Deployment:

- Confirm environment examples use placeholders only.
- Confirm production requires HTTPS, token encryption, strong DB credentials, secure cookies for web auth, and reverse-proxy headers.
- Confirm Docker/systemd/reverse proxy configs do not expose admin ports, database ports, profiling endpoints, or debug logs.

## Dynamic API Checks

- Use local or staging targets first.
- Run ZAP baseline/passive scan for headers, cache, cookies, mixed content, private IP disclosure, and common API mistakes.
- For authenticated mobile endpoints, use a disposable token and avoid checking secrets into reports.
- Do not run aggressive active scans against production without explicit user authorization.

## Triage

Severity guidance:

- Critical: unauthenticated access to private Strava data, token leakage, exploitable auth bypass, live secret committed to history.
- High: bearer token sent over HTTP in release, missing athlete scoping with data exposure, production token encryption disabled, OAuth state bypass.
- Medium: personal/publishing metadata residue, public-host config foot-guns, weak rate limits, excessive error detail, cache scoping that will break multi-user.
- Low: hardening gaps, documentation gaps, optional privacy controls, scanner findings with low exploitability.

Always separate confirmed vulnerabilities from scanner leads.
