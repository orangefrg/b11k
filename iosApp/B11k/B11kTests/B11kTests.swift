import Foundation
import Testing
@testable import B11k

@MainActor
struct B11kTests {
    private func job(state: String, phase: String = "importing", discoveryDone: Bool = true) throws -> SyncJob {
        let json = """
        {"job":{"id":"0123456789abcdef0123456789abcdef","state":"\(state)","phase":"\(phase)",
        "discovery_done":\(discoveryDone),"next_attempt_at":"2026-09-21T10:15:01.123456789Z",
        "message":"Saved progress","last_successful_at":"2026-09-20T10:00:00Z",
        "summary":{"total":5,"existing":2,"new":3,"success":2,"failed":1,"pending":0},
        "failures":[{"activity_id":77,"message":"Strava returned HTTP 403"}]}}
        """
        return try #require(JSONDecoder.b11k.decode(SyncJobResponse.self, from: Data(json.utf8)).job)
    }

    @Test func restoredProgressKeepsPartialFailureAndLastSuccess() throws {
        let restored = try job(state: "partial")
        #expect(restored.summary.total == 5)
        #expect(restored.summary.failed == 1)
        #expect(restored.failures.first?.activityID == 77)
        #expect(restored.lastSuccessfulAt != nil)
        #expect(restored.canRetry)
        #expect(!restored.shouldPoll)
        #expect(!restored.isActive)
    }

    @Test func quotaWaitKeepsJobActive() throws {
        let waiting = try job(state: "waiting")
        #expect(waiting.isActive)
        #expect(waiting.shouldPoll)
        #expect(!waiting.canRetry)
    }

    @Test func authorizationWaitOffersRecoveryWithoutEndlessPolling() throws {
        let paused = try job(state: "needs_auth")
        #expect(paused.isActive)
        #expect(!paused.shouldPoll)
        #expect(paused.canRetry)
        #expect(paused.title == "Reconnect Strava")
    }

    @Test func cancellationCanResumeAndDiscoveryHasNoFalseCompletion() throws {
        #expect(try job(state: "cancelled").canRetry)
        let discovering = try job(state: "running", phase: "discovering", discoveryDone: false)
        #expect(!discovering.discoveryDone)
        #expect(discovering.title == "Finding activities")
        #expect(discovering.shouldPoll)
    }

    @Test func noPreviousJobIsAValidResponse() throws {
        let response = try JSONDecoder.b11k.decode(SyncJobResponse.self, from: Data(#"{"job":null}"#.utf8))
        #expect(response.job == nil)
    }
}
