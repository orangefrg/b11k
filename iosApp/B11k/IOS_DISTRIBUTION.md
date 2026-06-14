# B11K iOS Distribution Notes

This file describes how to move the iOS app from personal development to
TestFlight and then App Store distribution.

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
3. Enable iCloud and CloudKit for the app target.
4. Create or select the CloudKit container.
5. Enable associated domains if using universal links.
6. Add URL scheme support if using custom-scheme Strava callback.
7. Create the app record in App Store Connect.
8. Archive in Xcode.
9. Upload the archive to App Store Connect.
10. Add internal testers in TestFlight.
11. Test login, CloudKit sync, import/export, segment creation, and API access
    on at least two devices signed into the same iCloud account.

Backend changes before TestFlight:

- Public HTTPS API endpoint available without Cloudflare Access browser SSO.
- Backend app-session auth enabled.
- Strava refresh-token storage enabled.
- Secure cookies enabled for web auth.
- Cross-user tests passing.

CloudKit steps before external testers:

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
