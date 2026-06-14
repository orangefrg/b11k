# B11K iOS Implementation Plan

This plan is based on the current repository state after the web app moved to
MapLibre/OpenFreeMap style maps and added the Discovered map feature.

## Current State

- The web app is a Go server with server-rendered templates and JSON APIs.
- Activity, route, segment, effort, graph, HR zone, and discovered-map data are
  backed by PostgreSQL/PostGIS.
- The web map uses MapLibre GL with `web/static/map-style.json`, which points at
  OpenFreeMap/OpenMapTiles sources.
- Favorite segments are stored in `favorite_segments`, scoped by `athlete_id`.
- Segment effort matching and metrics are computed in PostGIS and cached in
  `segment_activity_matches`.
- The Discovered map is backed by `discovered_activity_buffers` and
  `discovered_coverage_cache`.
- `iosApp/B11k` is currently a SwiftUI/SwiftData starter project with CloudKit
  entitlement scaffolding but no app-specific functionality yet.

## Important Backend Gap Before iOS

The web server still keeps Strava auth state in process-wide fields:

- `server.token`
- `server.user`

That is acceptable only for a single personal web process. Before a native app
or multiple users, auth must become request-scoped and persisted.

Recommended backend changes:

1. Add `athletes`, `strava_tokens`, and `app_sessions` tables.
2. Store Strava access token, refresh token, expiry, granted scopes, and athlete
   metadata per Strava athlete.
3. Refresh Strava access tokens server-side before calling Strava.
4. Replace process-wide `s.token` and `s.user` with a request session resolved
   from a secure cookie or `Authorization: Bearer <b11k-session-token>`.
5. Keep all database reads and writes athlete-scoped.
6. Use a connection pool instead of one serialized `pgx.Conn` once the mobile
   API and multi-user usage are introduced.

## Segment Cloud Strategy

Use two layers:

1. Backend Postgres remains authoritative for Strava-derived data and
   computation-heavy features.
2. CloudKit private database stores the user's segment library for iOS
   cross-device sync.

Why this hybrid is best for now:

- CloudKit gives iPhone/iPad sync through the user's iCloud account.
- PostGIS remains the right home for matching segments against activities,
  building metrics, and serving the web app.
- Export/import remains platform-neutral.
- Later, if CloudKit becomes inconvenient for App Store or Android/web parity,
  the backend can become the single source of truth for segments without
  rewriting the geospatial core.

CloudKit records should use stable IDs independent of Postgres IDs:

- `Segment`
  - `segmentUUID`
  - `athleteID`
  - `name`
  - `description`
  - `coordinatesJSON`
  - `elevationGainM`
  - `elevationLossM`
  - `netElevationM`
  - `createdAt`
  - `updatedAt`
  - `source`
  - `backendSegmentID` optional cache/link

Sync behavior:

- iOS reads/writes local SwiftData models synced to CloudKit.
- On app launch or segment change, iOS reconciles CloudKit segments with the
  backend: create missing backend rows, update changed rows, and delete only
  when explicitly requested.
- Backend returns Postgres IDs and computed metrics; iOS keeps them as cache.
- If the same segment exists in CloudKit but not in backend, import it into
  backend and recompute matches.

Free Apple Account caveat:

- Keep CloudKit behind a feature flag while using a personal/free Apple account.
- Xcode may allow local device development with a Personal Team, but some iCloud
  and CloudKit setup paths require an active Apple Developer Program membership
  and a configured iCloud container.
- The app should still run without CloudKit by keeping local SwiftData storage
  and backend segment sync available.

## Segment Export And Import

Add backend and iOS support for a versioned JSON file:

```json
{
  "format": "b11k_segments",
  "version": 1,
  "exported_at": "2026-05-24T00:00:00Z",
  "segments": [
    {
      "segment_uuid": "uuid",
      "name": "Segment name",
      "description": "",
      "geometry": {
        "type": "LineString",
        "coordinates": [[20.0, 44.0], [20.1, 44.1]]
      },
      "elevation_gain_m": 120.0,
      "elevation_loss_m": 10.0,
      "net_elevation_m": 110.0
    }
  ]
}
```

Rules:

- Export geometry and metadata, not athlete-specific efforts.
- Import creates new backend rows for the importing athlete and recomputes all
  matches against that athlete's activities.
- Preserve `segment_uuid` when possible, but never rely on Postgres numeric IDs
  across users.

Recommended endpoints:

- `GET /api/v1/segments/export`
- `GET /api/v1/segments/{id}/export`
- `POST /api/v1/segments/import`

## Native iOS Feature Plan

Build the iOS app as a native client over a versioned backend API, with local
SwiftData cache and optional CloudKit sync.

Milestone 1: Foundation

- Replace the starter `Item` model with app models:
  - `CachedActivity`
  - `CachedPointSample`
  - `CachedSegment`
  - `CachedSegmentEffort`
  - `SyncState`
- Add `B11KAPIClient`.
- Add Keychain storage for app session token.
- Add environment config for local, personal production, and future App Store
  API base URLs.

Milestone 2: Auth

- Add `ASWebAuthenticationSession` Strava login.
- Prefer backend token exchange so the Strava client secret never ships in iOS.
- Backend creates a B11K session after exchanging Strava code.
- iOS stores only the B11K session token in Keychain.

Milestone 3: Activities

- Activities list with pagination/search/filter.
- Activity detail with stats, route map, metric coloring, point inspection, and
  graph data.
- Sync screen with progress events. Use SSE if practical; otherwise add a
  backend job endpoint plus polling.

Milestone 4: Segments

- Segments list and detail.
- Efforts list with sorting and tolerance.
- Segment graph and per-effort metrics.
- Segment creation from an activity route by selecting start and end points.
- Export/import through `ShareLink` and document picker.

Milestone 5: Discovered Map

- Add status/rebuild actions.
- Render coverage/fog overlays from `/api/discovered/status`,
  `/api/discovered/coverage`, and `/api/discovered/fog`.

Milestone 6: Multi-User Readiness

- Ensure every endpoint resolves the athlete from the B11K session.
- Remove all process-global user/token state.
- Add tests for cross-user isolation.

## Map Recommendation

Use MapKit for normal activity and segment maps if it can cover the needed
native interactions:

- Polyline route rendering.
- Start/finish markers.
- Segment selection by tapping nearest route point.
- Metric-colored route segments by drawing many `MKPolyline` overlays.
- Coverage/fog polygons for Discovered map.

MapKit advantages:

- Native feel.
- No external tile dependency.
- App Store-friendly.
- Good for iOS-only personal use.

MapKit tradeoffs:

- Styling is less flexible than MapLibre.
- Dense metric-colored routes may require performance tuning.
- The Discovered fog overlay may be easier with MapLibre if polygons become
  large or visually complex.

Fallback if MapKit is not enough:

- Use MapLibre Native for iOS and the same OpenFreeMap/OpenMapTiles style logic
  as the web app.
- Avoid Mapbox unless you specifically want Mapbox services again.

For future Android or cross-platform clients, MapLibre is the better parity
choice because the current web map is already MapLibre-style and OSM-compatible.

## Deployment And Strava Auth Plan

For the current personal setup:

- Keep the web UI behind cloudflared + SSO.
- Add a separate public HTTPS API/auth surface for the iOS app, for example:
  - `https://b11k.example.com/auth/*`
  - `https://b11k.example.com/api/v1/*`
- Protect that public API with B11K session tokens, not Cloudflare Access SSO.
- Keep the web-only pages behind Cloudflare Access if desired.

Why:

- Native apps should not embed Cloudflare Access service tokens.
- Strava needs a stable callback URL or mobile redirect URI.
- The backend must own the Strava client secret.

Recommended Strava flow:

1. iOS calls backend to start login.
2. Backend returns a Strava authorization URL and state.
3. iOS opens it with `ASWebAuthenticationSession`.
4. Strava redirects back with a code.
5. iOS sends code and state to backend.
6. Backend exchanges code for Strava tokens.
7. Backend stores/rotates Strava refresh token.
8. Backend returns a B11K app session token.

For web login, keep `/strava/login` and `/strava/callback`, but move them to
the same persisted token/session model.

## Remaining Questions

1. What bundle identifier do you want long-term? Use the final one early because
   CloudKit container names and App Store migration are easier if it is stable.
2. Should iOS sync CloudKit segments automatically to backend on every change,
   or require a visible "Sync segments" action for the personal phase?
3. Should Discovered map be part of the first iOS version, or come after
   Activities and Segments are solid?
4. Do you want backend sessions to expire quickly, or is a long-lived personal
   session acceptable until TestFlight?
