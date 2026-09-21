import Foundation

struct SyncJobResponse: Decodable {
    let job: SyncJob?
}

struct SyncJob: Decodable, Identifiable {
    let id: String
    let state: String
    let phase: String
    let discoveryDone: Bool
    let nextAttemptAt: Date
    let message: String
    let lastSuccessfulAt: Date?
    let summary: SyncSummary
    let failures: [Failure]

    struct Failure: Decodable, Identifiable {
        let activityID: Int64
        let message: String
        var id: Int64 { activityID }
        enum CodingKeys: String, CodingKey { case activityID = "activity_id", message }
    }

    enum CodingKeys: String, CodingKey {
        case id, state, phase, message, summary, failures
        case discoveryDone = "discovery_done"
        case nextAttemptAt = "next_attempt_at"
        case lastSuccessfulAt = "last_successful_at"
    }

    var isActive: Bool { ["queued", "running", "waiting", "needs_auth"].contains(state) }
    var shouldPoll: Bool { ["queued", "running", "waiting"].contains(state) }
    var canRetry: Bool { ["failed", "partial", "needs_auth", "cancelled"].contains(state) }
    var title: String {
        switch state {
        case "queued": "Queued"
        case "waiting": "Waiting to retry"
        case "needs_auth": "Reconnect Strava"
        case "complete": "Sync complete"
        case "partial": "Some activities need attention"
        case "failed": "Sync needs attention"
        case "cancelled": "Sync cancelled"
        default:
            switch phase {
            case "discovering": "Finding activities"
            case "finalizing": "Updating discovered map"
            default: "Importing activities"
            }
        }
    }
}
