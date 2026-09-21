import Foundation
import Testing
@testable import B11k

@MainActor
private final class AppHarness {
    let defaults: UserDefaults
    let suite = "B11kTests.\(UUID().uuidString)"
    var requests: [URLRequest] = []
    var savedTokens: [String] = []
    var handler: (URLRequest) async throws -> (Int, String) = { _ in (200, #"{"job":null}"#) }
    var model: AppViewModel!

    init(token: String = "test-bearer", legacy: String? = nil) {
        defaults = UserDefaults(suiteName: suite)!
        defaults.set("https://backend.example", forKey: "b11k.baseURL")
        if let legacy { defaults.set(legacy, forKey: "b11k.sessionToken") }
        model = AppViewModel(defaults: defaults, loadToken: { token }, saveToken: { [unowned self] in savedTokens.append($0) }, transport: { [unowned self] request in
            requests.append(request)
            let (status, body) = try await handler(request)
            return (Data(body.utf8), HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!)
        })
    }

    func cleanup() {
        model.setSyncAppActive(false)
        defaults.removePersistentDomain(forName: suite)
    }

    static func job(_ state: String, success: Int = 0) -> String {
        """
        {"job":{"id":"0123456789abcdef0123456789abcdef","state":"\(state)","phase":"importing",
        "discovery_done":true,"next_attempt_at":"2026-09-21T10:15:01Z","message":"Saved progress",
        "last_successful_at":null,"summary":{"total":\(success),"existing":0,"new":\(success),"success":\(success),"failed":0,"pending":0},"failures":[]}}
        """
    }
}

@MainActor
struct AppViewModelTests {
    @Test func manualSyncDefaultsToAllHistoryAndSendsBearer() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.model.setSyncAppActive(false)
        h.handler = { _ in (202, AppHarness.job("queued")) }
        await h.model.sync()
        #expect(h.requests.count == 1)
        #expect(h.requests.first?.httpMethod == "POST")
        #expect(h.requests.first?.url?.path == "/api/mobile/sync/jobs")
        #expect(h.requests.first?.url?.query == nil)
        #expect(h.requests.first?.value(forHTTPHeaderField: "Authorization") == "Bearer test-bearer")
        #expect(h.requests.first?.timeoutInterval == 30)
        #expect(h.model.isSyncing)
        #expect(!h.model.isBusy) // Browsing remains available during an import.
    }

    @Test func invalidDateRangeDoesNotStartAJob() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.model.syncDateRangeEnabled = true
        h.model.startDate = Date(timeIntervalSince1970: 100)
        h.model.endDate = Date(timeIntervalSince1970: 0)
        await h.model.sync()
        #expect(h.requests.isEmpty)
        #expect(h.model.showingMessage)
        #expect(!h.model.isChangingSync)
    }

    @Test func lostStartResponseChecksSavedJobWithoutPostingAgain() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { request in
            if request.httpMethod == "POST" { throw URLError(.networkConnectionLost) }
            return (200, AppHarness.job("needs_auth"))
        }
        await h.model.sync()
        await h.model.resumeSyncMonitoring()
        #expect(h.requests.filter { $0.httpMethod == "POST" }.count == 1)
        #expect(h.model.syncJob?.state == "needs_auth")
        #expect(h.model.isAuthorized)
        #expect(h.model.syncConnectionMessage.isEmpty)
    }

    @Test func cancelledSyncCanResumeTheSameJob() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (200, AppHarness.job("needs_auth")) }
        await h.model.resumeSyncMonitoring()
        h.model.setSyncAppActive(false)
        h.handler = { request in (200, AppHarness.job(request.url!.path.hasSuffix("/cancel") ? "cancelled" : "queued")) }
        await h.model.changeSync("cancel")
        #expect(h.model.syncJob?.state == "cancelled")
        #expect(!h.model.isSyncing)
        await h.model.changeSync("retry")
        #expect(h.model.syncJob?.state == "queued")
        #expect(h.requests.suffix(2).allSatisfy { $0.httpMethod == "POST" && $0.url!.path.contains("0123456789abcdef0123456789abcdef/") })
    }

    @Test func failedCancellationKeepsSavedProgressAndAllowsRetry() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (200, AppHarness.job("needs_auth")) }
        await h.model.resumeSyncMonitoring()
        h.handler = { _ in (503, "Unavailable") }
        await h.model.changeSync("cancel")
        #expect(h.model.syncJob?.state == "needs_auth")
        #expect(!h.model.isChangingSync)
        #expect(!h.model.syncConnectionMessage.isEmpty)
    }

    @Test func backgroundDoesNotPollAndForegroundRestoresProgress() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (200, AppHarness.job("needs_auth")) }
        h.model.setSyncAppActive(false)
        await h.model.resumeSyncMonitoring()
        #expect(h.requests.isEmpty)
        h.model.setSyncAppActive(true)
        await h.model.resumeSyncMonitoring()
        #expect(h.requests.count == 1)
        #expect(h.model.syncJob?.state == "needs_auth")
    }

    @Test func oldBackendResponseCannotReplaceNewBackendState() async {
        let h = AppHarness(); defer { h.cleanup() }
        var pending: CheckedContinuation<Void, Never>?
        h.handler = { _ in
            await withCheckedContinuation { pending = $0 }
            return (200, AppHarness.job("needs_auth"))
        }
        let request = Task { await h.model.resumeSyncMonitoring() }
        while pending == nil { await Task.yield() }
        h.model.baseURLString = "https://different.example"
        h.model.resetSyncMonitoring()
        pending?.resume()
        await request.value
        #expect(h.model.syncJob == nil)
        #expect(h.model.syncConnectionMessage.isEmpty)
    }

    @Test func expiredSessionClearsCredentialsAndJob() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (401, "Session expired") }
        await h.model.resumeSyncMonitoring()
        #expect(!h.model.isAuthorized)
        #expect(h.savedTokens == [""])
        #expect(h.model.syncJob == nil)
        #expect(h.model.showingMessage)
    }

    @Test func uncertainLogoutKeepsSessionForRetry() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (503, "Unavailable") }
        await h.model.logout()
        #expect(h.model.isAuthorized)
        #expect(h.savedTokens.isEmpty)
        #expect(!h.model.isLoggingOut)
        h.handler = { _ in (200, #"{"logged_out":true}"#) }
        await h.model.logout()
        #expect(!h.model.isAuthorized)
        #expect(h.savedTokens == [""])
    }

    @Test func neverSendsBearerOverPublicHTTP() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.model.baseURLString = "http://public.example"
        await h.model.loadMe()
        #expect(h.requests.isEmpty)
        #expect(h.model.isAuthorized)
        #expect(h.model.showingMessage)
    }

    @Test func legacyTokenMigratesOnceWithoutTouchingRealKeychain() {
        let h = AppHarness(token: "", legacy: "legacy-token"); defer { h.cleanup() }
        #expect(h.model.isAuthorized)
        #expect(h.savedTokens == ["legacy-token"])
        #expect(h.defaults.string(forKey: "b11k.sessionToken") == nil)
    }

    @Test func malformedStatusPreservesAuthorizationAndExplainsReconnect() async {
        let h = AppHarness(); defer { h.cleanup() }
        h.handler = { _ in (200, "not JSON") }
        await h.model.resumeSyncMonitoring()
        #expect(h.model.isAuthorized)
        #expect(h.model.syncJob == nil)
        #expect(!h.model.syncConnectionMessage.isEmpty)
    }
}
