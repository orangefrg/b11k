#if DEBUG
import Foundation

/// In-memory HTTP boundary for UI automation. No Keychain, account, or backend is used.
/// Unknown routes and malformed/unauthorized requests fail closed.
@MainActor
final class UITestBackend {
    private var segmentName = "River climb"
    private var segmentDescription = "Between two bridges"
    private var deleted = false
    private var failNextEdit = true
    private var failNextCancel = true
    private var syncState: String?
    private var interruptNextPoll = false
    private var startCount = 0

    static let points: [[String: Any]] = (0..<9).map { index -> [String: Any] in
        let latitude = 44.80 + Double(index) * 0.002
        let longitude = 20.44 + Double(index) * 0.003
        return ["index": index, "lat": latitude, "lng": longitude,
                "altitude": 80 + index * 10, "heartrate": 100 + index * 10,
                "speed": 3 + index, "cadence": 70 + index, "cumulative_distance": index * 1000]
    }

    private var segment: [String: Any] {
        ["id": 7, "name": segmentName, "description": segmentDescription,
         "created_at": "2026-09-20", "updated_at": "2026-09-21", "direction": "Uphill",
         "direction_key": "uphill", "distance_meters": 8000, "slope_percent": 1,
         "geometry": ["type": "LineString",
                      "coordinates": Self.points.map { [$0["lng"]!, $0["lat"]!] },
                      "points": Self.points]]
    }

    func respond(to request: URLRequest) async throws -> (Data, URLResponse) {
        guard let url = request.url, url.host == "ui-test.invalid",
              request.value(forHTTPHeaderField: "Authorization") == "Bearer ui-test-token" else {
            throw URLError(.userAuthenticationRequired)
        }
        func reply(_ json: Any, status: Int = 200) throws -> (Data, URLResponse) {
            (try JSONSerialization.data(withJSONObject: json),
             HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!)
        }
        let method = request.httpMethod ?? "GET"
        switch (method, url.path) {
        case ("GET", "/api/mobile/me"):
            return try reply(["athlete": ["id": 123, "firstname": "Test", "lastname": "Cyclist"]])
        case ("GET", "/api/mobile/activities"):
            var activities: [[String: Any]] = [
                ["id": 42, "name": "River morning ride", "type": "Ride", "start_date": "2026-09-20T09:00:00Z", "distance": 8000, "moving_time": 480],
                ["id": 43, "name": "Hill evening ride", "type": "Ride", "start_date": "2026-09-19T09:00:00Z", "distance": 6000, "moving_time": 600]
            ]
            if syncState == "complete" {
                activities.insert(["id": 44, "name": "Newly imported ride", "type": "Ride", "distance": 9000], at: 0)
            }
            let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "q" }?.value ?? ""
            if !query.isEmpty { activities = activities.filter { ($0["name"] as! String).localizedCaseInsensitiveContains(query) } }
            return try reply(["activities": activities, "count": activities.count, "has_more": false])
        case ("GET", "/api/mobile/activities/42/route"), ("GET", "/api/mobile/activities/43/route"):
            return try reply(["activity_id": 42, "source": "point_samples", "count": Self.points.count, "points": Self.points])
        case ("GET", "/api/mobile/segments"):
            return try reply(["segments": deleted ? [] : [segment]])
        case ("GET", "/api/mobile/segments/7"):
            return try reply(["segment": segment])
        case ("GET", "/api/mobile/segments/7/activities"):
            return try reply(["segment_id": 7, "activities": []])
        case ("PATCH", "/api/mobile/segments/7"):
            if failNextEdit {
                failNextEdit = false
                return try reply(["error": "Segment save unavailable. Please retry."], status: 503)
            }
            guard let body = request.httpBody,
                  let object = try JSONSerialization.jsonObject(with: body) as? [String: Any],
                  let name = object["name"] as? String, !name.isEmpty,
                  let description = object["description"] as? String else {
                return try reply(["error": "Invalid edit payload"], status: 400)
            }
            segmentName = name
            segmentDescription = description
            return try reply(["segment": segment])
        case ("DELETE", "/api/mobile/segments/7"):
            deleted = true
            return try reply([:])
        case ("POST", "/api/mobile/sync/jobs"):
            startCount += 1
            guard startCount == 1, url.query == nil else {
                return try reply(["error": "Expected one all-history sync"], status: 400)
            }
            syncState = "waiting"
            return try reply(jobResponse)
        case ("POST", "/api/mobile/sync/jobs/ui-job/cancel"):
            if failNextCancel {
                failNextCancel = false
                return try reply(["error": "Cancel unavailable. Please retry."], status: 503)
            }
            syncState = "cancelled"
            return try reply(jobResponse)
        case ("POST", "/api/mobile/sync/jobs/ui-job/retry"):
            guard syncState == "cancelled" else { return try reply(["error": "Wrong job state"], status: 409) }
            syncState = "running"
            interruptNextPoll = true
            return try reply(jobResponse)
        case ("GET", "/api/mobile/sync/jobs"):
            if interruptNextPoll {
                interruptNextPoll = false
                throw URLError(.networkConnectionLost)
            }
            if syncState == "running" { syncState = "complete" }
            return try reply(jobResponse)
        case ("GET", "/api/mobile/discovered/status"):
            return try reply(["cached_activities": 3, "stale": false])
        default:
            return try reply(["error": "Unstubbed UI test request: \(method) \(url.path)"], status: 500)
        }
    }

    private var jobResponse: [String: Any] {
        guard let syncState else { return ["job": NSNull()] }
        return ["job": ["id": "ui-job", "state": syncState, "phase": "importing",
                        "discovery_done": true, "next_attempt_at": "2026-09-21T09:00:00Z",
                        "message": syncState == "waiting" ? "Waiting for Strava quota reset." : "Saved progress restored.",
                        "summary": ["total": 3, "existing": 1, "new": 2, "success": syncState == "complete" ? 2 : 1,
                                    "failed": 0, "pending": syncState == "complete" ? 0 : 1], "failures": []]]
    }
}
#endif
