# B11K iOS Distribution Notes

This file describes how to move the iOS app from personal development to
TestFlight and then App Store distribution.

## Command-line Internal TestFlight

From the repository root, use the same interface as Skupobrate:

```bash
./scripts/release-apple --version 1.0 --build 3 --distribute testflight \
  --env-file secrets/apple-release.env --signing-auth xcode
```

`1.0 (3)` is an example; choose an unused build number. The script runs
`B11kTests` on the iPhone 17 Pro simulator, creates and validates a Release
archive, exports an IPA, uploads it, waits for Apple processing, and verifies
internal TestFlight readiness. Version/build settings are overridden for that
run; the saved Xcode project is not changed. Xcode's automatic build-number
management is disabled.

All exports set `testFlightInternalTestingOnly=true`. These builds cannot be
used for external testing or App Store submission. The script accepts only
existing internal groups and does not create groups or invite testers. See
[Apple's internal testing guide](https://developer.apple.com/help/app-store-connect/test-a-beta-version/add-internal-testers).

### One-time setup

1. Use macOS with full Xcode selected, Python 3.9+, OpenSSL, and an installed
   iOS simulator compatible with the app's deployment target. No Fastlane or
   third-party Python packages are needed.
2. Configure paid-team signing and an App Store Connect app record for
   `com.apetrikov.b11k`. The script defaults to the team in the Xcode project
   (currently `H77N8JK4W4`, the team that owns `com.apetrikov.b11k`). Override it using `--team-id TEAM_ID` or
   `APPLE_TEAM_ID` if the app belongs to another team. Reusing a Skupobrate API
   key does not change B11K's signing team.
3. Create an internal testing group in App Store Connect. Enable automatic
   distribution, or pass its exact name with `--group "Internal Testers"`.
   Repeat `--group` for multiple internal groups. The script checks this before
   building so a successful upload has a distribution destination.
4. Supply an App Store Connect **team API key** with access to B11K and beta
   distribution permissions. Individual API keys are not supported. Create
   the local credential file:

   ```bash
   mkdir -p secrets
   cp scripts/apple-release.env.example secrets/apple-release.env
   ```

   Edit it to contain:

   ```dotenv
   ASC_KEY_PATH="./AuthKey_YOUR_KEY_ID.p8"
   ASC_KEY_ID="YOUR_KEY_ID"
   ASC_ISSUER_ID="YOUR_ISSUER_ID"
   ```

   Put the `.p8` next to that file, or use an absolute key path. The script
   reads assignments literally: it never executes shell expressions. All
   three values must come from the selected file. Without `--env-file`, it
   reads those three environment variables instead. `secrets/`, key files,
   and release artifacts are Git-ignored.

An existing credential file can be reused if its key has access to this app:

```bash
./scripts/release-apple --version 1.0 --build 3 --distribute testflight \
  --env-file ../skupobrate/secrets/apple-release.env --signing-auth xcode
```

`--signing-auth xcode` uses the account in Xcode Settings → Accounts for
signing/upload, while the API key checks processing and assigns groups. The
default `--signing-auth api-key` also supplies that key to Xcode. Automatic
provisioning is enabled for signed operations; disable it with
`--no-provisioning-updates` only when signing material is already installed.
Local archive/export modes can use an existing Xcode account without API keys.

### Preview, artifacts, and retries

```bash
# Preview without building or contacting Apple; no credentials required.
./scripts/release-apple --version 1.0 --build 3 --distribute testflight --dry-run

# Local unsigned archive and unit tests; no upload or provisioning.
./scripts/release-apple --version 1.0 --build 3 --unsigned \
  --output-dir /tmp/b11k-release-check

# Continue a signed release using its original version/build and artifacts.
./scripts/release-apple --version 1.0 --build 3 --distribute testflight \
  --env-file secrets/apple-release.env --signing-auth xcode --resume
```

Artifacts default to `dist/apple/VERSION-BUILD/`: `ios.xcarchive`, exported
IPA, per-step `logs/`, DerivedData, and `release.json` recording progress.
Use `--output-dir PATH` to change this. Paths are relative to the caller's
working directory; relative key paths are relative to the credential file.
An explicitly selected credential file is validated even during a dry run.
Credential arguments are redacted from the console; treat Xcode logs as private.

Use `--distribute archive` (the default) for a local archive, or `--distribute
export` for an archive and local IPA. A signed archive/export can be uploaded
later with `--resume --distribute testflight`. An unsigned archive cannot be
promoted to a signed release. Keep the same version, build, team, and output
directory on resume, and repeat your credential/signing/group options.

Completed steps are reused. Source changes block resuming incomplete builds;
completed archives can be reused even after the checkout changes. After an
uncertain upload, resume first looks for the existing build at Apple. It does
not automatically resend it. Only after confirming Apple did not receive it,
use `--resume --retry-upload`. A processing timeout or missing export-compliance
answer preserves the upload; resolve it in App Store Connect and resume.

Other options: `--ios-test-destination 'platform=iOS Simulator,id=UUID'`
(or `IOS_TEST_DESTINATION`), `--skip-tests`, and `--processing-timeout 2400`
(default 1200 seconds). Run `--help` for all options.

The current app unit suite is a starter test; passing it is not functional beta
qualification. Test login, sync, maps, and segments on a real device separately.
CloudKit and segment import/export are not implemented or required for this
backend-backed internal build.

Run the release utility's offline regression tests with:

```bash
python3 -B -m unittest discover -s scripts/tests -v
```

## Phase 1: Personal Device Development

Goal: run the app on your own iPhone while the backend and app are still moving.

Steps:

1. Open `iosApp/B11k/B11k.xcodeproj` in Xcode.
2. Select the `B11k` target.
3. In Signing & Capabilities, select your Personal Team.
4. Set a stable bundle identifier, for example `com.example.b11k`.
5. Keep CloudKit optional at runtime until signing confirms the iCloud
   container is available.
6. Run on a connected device from Xcode.

Limitations:

- Personal Team signing is for local development, not distribution.
- Builds may need to be refreshed periodically.
- Some capabilities, CloudKit setup, App Store Connect, TestFlight, and App
  Store distribution require Apple Developer Program membership.
- For local Personal Team builds, keep `B11k.entitlements` empty. Re-enable
  iCloud/CloudKit only after switching to a paid developer team.

## Phase 2: Prepare For Apple Developer Program

Do this before paying or immediately after enrollment:

1. Decide final bundle identifier.
2. Decide final display name.
3. Decide CloudKit container name, normally `iCloud.<bundle-identifier>`.
4. Decide backend production hostname.
5. Decide Strava callback domain and mobile redirect URI.
6. Add privacy notes for Strava activity data and iCloud segment storage.

Keep secrets out of the app:

- Do not ship the Strava client secret in iOS.
- Store the Strava client secret only on the backend.
- Store the B11K app session token in the iOS Keychain.

## Phase 3: TestFlight

Requires active Apple Developer Program membership.

Steps:

1. Sign in to Xcode with the paid developer account.
2. Select the paid Team in Signing & Capabilities.
3. Keep iCloud/CloudKit disabled for the current backend-backed app.
4. Configure a CloudKit container only if a future build implements that sync.
5. Enable associated domains if using universal links.
6. Add URL scheme support if using custom-scheme Strava callback.
7. Create the app record in App Store Connect.
8. Archive in Xcode.
9. Upload the archive to App Store Connect.
10. Add internal testers in TestFlight.
11. Test login, activity sync, maps, segment creation, and API access on at
    least two devices. Add CloudKit and import/export checks when implemented.

Backend changes before TestFlight:

- Public HTTPS API endpoint available without Cloudflare Access browser SSO.
- Backend app-session auth enabled.
- Strava refresh-token storage enabled.
- Secure cookies enabled for web auth.
- Cross-user tests passing.

For a future CloudKit-enabled build, before external testers:

- Confirm records sync in the development CloudKit environment.
- Promote CloudKit schema to production.
- Confirm a fresh install can read/write segments without development-only
  records.

## Phase 4: App Store

Before review:

1. Make the backend production URL configurable by build configuration but fixed
   for release builds.
2. Verify App Transport Security uses HTTPS only.
3. Add a privacy policy URL.
4. Explain Strava data usage clearly:
   - activity data is used to show routes, stats, graphs, and segment efforts;
   - segment definitions may sync through the user's iCloud account;
   - activity data is stored on the B11K backend after the user authorizes
     Strava.
5. Add account deletion/data deletion instructions if multiple users are
   supported.
6. Make sure login works without requiring Cloudflare Access.
7. Make sure all third-party map tile terms are followed if using MapLibre/OSM.
8. Submit for review from App Store Connect.

## CloudKit Notes

Apple describes CloudKit as a way to store app data in iCloud and keep it synced
across devices. Apple also documents that enabling CloudKit in Xcode requires
the iCloud capability and a configured container.

Useful references:

- Apple CloudKit overview:
  https://developer.apple.com/icloud/cloudkit/
- Apple enabling CloudKit:
  https://developer.apple.com/documentation/cloudkit/enabling_cloudkit_in_your_app
- Apple membership comparison:
  https://developer.apple.com/support/compare-memberships/

Practical recommendation:

- Develop the app so it works with local SwiftData first.
- Add CloudKit as an optional sync layer.
- Treat paid Apple Developer Program enrollment as required before relying on
  CloudKit for TestFlight or App Store.

## Strava Auth Notes

Strava supports OAuth on web and mobile. For iOS, use
`ASWebAuthenticationSession` or the Strava mobile OAuth endpoint, then send the
authorization code to the backend for token exchange.

Useful reference:

- Strava authentication:
  https://developers.strava.com/docs/authentication/

Recommended callback options:

- Universal link: best long-term App Store option.
- Custom URL scheme: fine for personal/TestFlight if configured carefully.

For either option:

- The Strava callback domain must match the Strava app settings.
- Use `state` and validate it on return.
- Backend exchanges the code and stores refresh tokens.
- iOS receives only the B11K app session token.
