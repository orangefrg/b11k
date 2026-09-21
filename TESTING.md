# Testing and coverage

The backend, browser, and iOS suites exercise different layers. Their percentages
use different metrics and should not be added together. Tests use synthetic data;
no Strava login, production database, or App Store upload is required.

## Repeatable commands

```bash
# Go + Docker required. Creates and removes a private PostGIS test container.
./scripts/test-backend-coverage

# Node 22+ and pnpm required; one-time browser setup:
pnpm install --frozen-lockfile
pnpm test:install
./scripts/test-frontend

# macOS + Xcode with an iOS simulator; includes unit and UI tests:
./scripts/test-ios
# Faster request/model tests:
./scripts/test-ios --unit-only
# Choose another installed simulator:
./scripts/test-ios --destination 'platform=iOS Simulator,id=YOUR-UUID'

# Deployment and release-tool regressions, without Docker/Apple side effects:
python3 -B -m unittest discover -s scripts/tests
```

Backend output lives in `coverage/backend/`: `coverage.out`, function percentages,
and a browsable `index.html`. `COVERAGE_DIR` changes that output directory. The
command enables the race detector and explicitly instruments production packages
across package boundaries, so API tests count toward SQL helper coverage. It
excludes `internal/testdb`, whose code is only test infrastructure. Plain
`go test ./...` skips the database integration tests unless a test URL is supplied;
use the coverage/integration scripts for a representative assessment.

Browser output lives in `coverage/frontend/`: `summary.json` and raw `v8.json`.
The tests execute the actual `web/static/app.js` in Chromium with real DOM events,
production template fragments, and controlled network responses. Map and chart
scenarios use the same pinned MapLibre/Chart.js versions as the page templates,
served locally, with real WebGL/Canvas rendering and synthetic GPS/sensor data.
Software WebGL makes this repeatable without a physical GPU. The map style has
no external tiles; all unhandled requests are blocked. EventSource and the two
Discovered request-order scenarios retain deterministic test doubles.

This is browser behavior/rendering coverage, not a full server-to-browser test
or a check of the external basemap provider. V8 source offsets measure UTF-16
code units, not Go-style statements or branches. Earlier reports called these
"source bytes"; the calculation is unchanged and the label is now precise.

iOS output lives in a timestamped `coverage/ios/` directory: `tests.xcresult`,
`coverage.json`, `summary.json`, and logs. `--output-dir` selects a fresh directory. Open the result
bundle in Xcode to inspect individual tests and line coverage. The command reuses
the release script's simulator preparation and one-time preflight recovery, runs
serially, and keeps XCTest attachments without collecting a lengthy sysdiagnose
for ordinary assertion failures. Assertion failures are not automatically retried.

The view-model tests inject networking, session persistence, and UserDefaults.
UI tests use Debug-only signed-out and authenticated launch modes with separate
preferences and no Keychain or real backend access. The authenticated transport
validates request methods, paths, bearer credentials, and mutation bodies against
an in-memory account; unknown requests fail closed. MapKit renders the iOS route
normally, so Apple basemap loading is not part of the fixture or assertions.
Both launch modes and the fixture transport are absent from Release builds.
`summary.json` excludes `UITestBackend.swift` so fixture execution cannot inflate
app coverage; `coverage.json` retains the complete raw Xcode report.

## Assessment on 2026-09-21

| Layer | Before | After | Scope |
| --- | --- | --- | --- |
| Backend | 25.4% statements | 35.8% statements | All production Go packages, including the command entrypoint |
| Web JavaScript | No automated suite; first pass 10.3% source text | 20 passing browser scenarios; 58.6% source text | Entire `app.js`, including real activity and comparison map/chart interactions |
| iOS | 5 model tests; 10.1% app lines; first pass 26.1% | 20 unit/request/rendering tests + 6 UI tests; 66.7% app lines | Includes authenticated UI flows; excludes the Debug-only fixture transport |

The original iOS UI target contained launch/performance templates without product
assertions. It now checks signed-out navigation/setup, optional sync dates,
authenticated activity search, route colors/charts, segment creation validation,
failed segment edits with retained drafts, confirmed deletion, and sync recovery.

The backend assessment passed all 57 top-level Go tests with PostGIS integration
and race detection. This follow-up changed browser/iOS behavior and passed all
20 browser scenarios, 26 iOS checks, and 69 utility tests. The iOS Release simulator
build also passed; its binary contains neither the authenticated fixture transport
nor its test credentials/launch flag.

iOS coverage measures `B11k.app` on the iPhone 17 Pro simulator, excluding test
targets and `UITestBackend.swift`. It rose from 606/5,985 executable lines initially,
to 1,572/6,027 in the first pass, to 4,079/6,116 (66.7%) after this follow-up.

Backend package statement coverage changed as follows:

| Package | Before | After |
| --- | --- | --- |
| `internal/web` | 25.3% | 39.6% |
| `internal/pggeo` | 15.5% | 27.2% |
| `internal/sync` | 61.4% | 61.4% |
| `internal/strava` | 45.5% | 45.5% |
| `cmd` | 0.0% | 0.0% |

The five new database API flows cover pagination/search, route fallback and sensor
data, activity/segment ownership, segment creation/update/deletion, persisted
logout across devices, and Discovered coverage rebuild/isolation. Existing sync
tests continue checking transactional rollback, cancellation in flight, durable
retries, quotas, and credential rotation.

Browser scenarios cover sync discovery/import/finalization, quota waits, interrupted
streams, reattachment, successful completion, segment filtering/sorting/deletion,
page-size navigation, failed map rebuilds, and stale viewport responses. Rendering
scenarios check delayed route data, route gradients and HR zones, chart speed
conversion and tooltips, map-to-chart selection on both axes, endpoint ordering
and exclusive finish indices in segment creation, failed saves, effort comparison
alignment, and late responses after metrics or selected efforts change.

iOS request tests cover manual all-history sync, bearer transport, invalid date
ranges, uncertain POST recovery without duplicate starts, cancellation/resumption,
failed cancellation, foreground/background recovery, obsolete backend responses,
session expiry, failed logout, HTTP credential protection, legacy token migration,
and malformed status responses. Three MapKit unit tests verify that long colored
routes retain their finish, repaired interior points replace old geometry, and
missing metrics/invalid coordinates preserve a valid route or clear the map.

The authenticated UI scenarios exercise actual views through the request layer:
activity search and map/chart controls; a failed segment edit followed by retry
and cancel/confirm deletion; and manual all-history sync through a quota wait,
background/foreground restoration, failed cancellation, resume, a connection
interruption, and refreshed activities after completion.

## Regressions found and fixed

- An extreme activity page number overflowed an integer and panicked while slicing
  the result. Out-of-range pages now safely return an empty page.
- The browser expected obsolete sync phase names and showed 0% during imports.
  It understands durable-job phases and explains discovery without a fabricated
  overall percentage.
- Repeated web submissions kept multiple progress streams open. Reattachment now
  closes the old stream and ignores its late events.
- Browser transport failures displayed `Error: undefined`. They now explain that
  progress remains on the server and how to reconnect.

- Slow route/segment responses could miss MapLibre's load event and leave the map
  blank. Rendering now handles an already loaded map.
- Older graph/HR-zone responses could overwrite newer selections, and delayed
  effort responses could restore deselected comparison overlays/charts. Request
  generations keep only the current selection.
- Each chart recreation added another map-click listener. One listener now selects
  chart samples by timestamp on either axis. Graph request failures now show a
  recoverable message instead of a blank canvas.
- Long colored iOS routes could omit their final point after downsampling. The
  renderer retains the finish and invalidates cached geometry when interior points
  or sensor values change during a repair.
- A failed iOS segment save presented a global alert that dismissed the editor.
  Recoverable errors now appear inside the editor, preserving the draft for retry.

## Remaining gaps worth addressing next

- Browser checks do not validate external map tiles, cross-browser rendering,
  responsive layout, or every metric/data shape. The legacy single-effort graph
  path remains lightly covered.
- Authenticated Profile, Discovered, and detailed matched-effort iOS screens need
  further UI scenarios. Current fixtures cover activities, editing, and sync;
  they do not replace live provider/device checks.
- Real Strava OAuth, revoked grants, and provider contract changes still need a
  controlled account smoke test. Existing automated provider responses are mocked.
- CLI configuration/startup, schema validation/rebuild paths, and more geospatial
  matching/metric edge cases remain lightly tested. Prefer preservation and
  ownership invariants over tests that merely call every helper.
- Device-only behavior and accessibility at large text sizes need a dedicated
  physical-device/visual pass. Simulator line coverage cannot establish those.

No global coverage threshold is imposed yet: the mixed UI/SQL/legacy surface is
still sparse, and a percentage alone would overstate confidence. Keep the failure
scenarios passing and add checks alongside behavior changes. Dependencies,
fixtures, and reports stay out of deployment rsync transfers and Docker images.
