# Local Backend iPhone Testing

This first native iteration assumes the backend runs on your Mac and the iPhone
reaches it over the same Wi-Fi network.

## Backend

1. Set Strava credentials in `.env`.
2. Find your Mac's LAN IP address:

   ```sh
   ipconfig getifaddr en0
   ```

3. Set the iOS redirect URI to that LAN address:

   ```env
   B11K_IOS_REDIRECT_URI=http://<your-mac-lan-ip>:8080/api/mobile/auth/callback
   ```

4. Start the local stack:

   ```sh
   ./live-test.sh
   ```

5. On the iPhone, the backend URL should be:

   ```text
   http://<your-mac-lan-ip>:8080
   ```

## Strava App Settings

For this dev iteration, configure the Strava app callback domain to your Mac
LAN IP address. Strava wants the domain/host, not the full callback URL.

```text
<your-mac-lan-ip>
```

Example:

```text
10.0.0.42
```

## iPhone

1. Open `iosApp/B11k/B11k.xcodeproj` in Xcode.
2. Select your iPhone 13 Pro as the run destination.
3. Run the `B11k` target.
4. In the app, set Backend to `http://<your-mac-lan-ip>:8080`.
5. Tap `Connect Strava`.
6. Approve Strava.
7. The browser callback page should say Strava connected.
8. Return to B11K and tap `Check Login`.
9. Tap `Sync from Strava`.
10. Tap `Refresh Count` after sync if needed.

The app currently stores the B11K session token in app storage for development.
Backend sessions are in memory, so restarting the Go server invalidates the
native app session; just connect Strava again.
