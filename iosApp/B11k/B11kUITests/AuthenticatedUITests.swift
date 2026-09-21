import XCTest

@MainActor
final class AuthenticatedUITests: XCTestCase {
    private func launch() -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchArguments = ["--ui-testing", "--ui-testing-authenticated"]
        app.launch()
        XCTAssertTrue(app.buttons["activity-42"].waitForExistence(timeout: 10))
        return app
    }

    private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<8 {
            if element.exists && element.isHittable { return }
            // Scroll outside the MapKit canvas to avoid panning the route.
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.04, dy: 0.8))
                .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.04, dy: 0.25)))
        }
        XCTAssertTrue(element.isHittable, "Could not reveal \(element)")
    }

    private func expect(_ element: XCUIElement, value: String) {
        let predicate = NSPredicate(format: "label == %@", value)
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 10), .completed)
    }

    private func dismissMessage(_ app: XCUIApplication, containing text: String) {
        let message = app.alerts.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 5))
        app.alerts.buttons["OK"].tap()
    }

    func testActivitySearchRouteAndMetricControls() {
        let app = launch()
        let search = app.textFields["Name, place, gear"]
        search.tap()
        search.typeText("River")
        app.buttons["Search Activities"].tap()
        XCTAssertTrue(app.buttons["activity-42"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["activity-43"].exists)
        reveal(app.buttons["activity-42"], in: app)
        app.buttons["activity-42"].tap()
        XCTAssertTrue(app.navigationBars["Activity"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.maps.firstMatch.waitForExistence(timeout: 5))
        let paint = app.buttons["route-paint"]
        paint.tap()
        app.buttons["Speed"].tap()
        XCTAssertTrue(app.staticTexts["Route points"].exists)
        reveal(app.buttons["Create Segment"], in: app)
        XCTAssertTrue(app.buttons["Create Segment"].isEnabled)
        app.buttons["Create Segment"].tap()
        XCTAssertTrue(app.navigationBars["Create Segment"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.navigationBars.buttons["Save"].isEnabled)
        app.textFields["Name"].tap()
        app.textFields["Name"].typeText("Preview only")
        XCTAssertTrue(app.navigationBars.buttons["Save"].isEnabled)
        app.navigationBars.buttons["Cancel"].tap()
        XCTAssertTrue(app.navigationBars["Activity"].waitForExistence(timeout: 5))
        let metrics = app.segmentedControls.firstMatch
        reveal(metrics, in: app)
        metrics.buttons["Speed"].tap()
        let peak = app.staticTexts.containing(NSPredicate(format: "label MATCHES %@", "Top 39[.,]6 km/h")).firstMatch
        reveal(peak, in: app)
        XCTAssertTrue(peak.exists)
    }

    func testSegmentFailedEditPreservesDraftThenPersistsAndDeleteRequiresConfirmation() {
        let app = launch()
        app.tabBars.buttons["Segments"].tap()
        app.staticTexts["River climb"].tap()
        let edit = app.buttons["Edit Segment"]
        XCTAssertTrue(edit.waitForExistence(timeout: 5))
        edit.tap()
        let name = app.textFields["Name"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: "River climb".count) + "Bridge climb")
        app.navigationBars.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts["segment-save-error"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["segment-save-error"].label.contains("Segment save unavailable"))
        XCTAssertEqual(name.value as? String, "Bridge climb")
        app.navigationBars.buttons["Save"].tap()
        dismissMessage(app, containing: "Segment updated")
        XCTAssertTrue(app.staticTexts["Bridge climb"].waitForExistence(timeout: 5))
        app.buttons["Delete Segment"].tap()
        app.alerts["Delete Segment"].buttons["Cancel"].tap()
        XCTAssertTrue(app.staticTexts["Bridge climb"].exists)
        app.buttons["Delete Segment"].tap()
        app.alerts["Delete Segment"].buttons["Delete"].tap()
        dismissMessage(app, containing: "Segment deleted")
        XCTAssertTrue(app.staticTexts["No Segments"].waitForExistence(timeout: 5))
    }

    func testSyncCancelResumeAndInterruptedConnectionRefreshTheLibrary() {
        let app = launch()
        app.tabBars.buttons["Settings"].tap()
        let start = app.buttons["Sync from Strava"]
        reveal(start, in: app)
        XCTAssertTrue(start.isEnabled)
        start.tap()
        let state = app.staticTexts["sync-state"]
        expect(state, value: "Waiting to retry")
        XCTAssertFalse(start.isEnabled)
        XCTAssertTrue(app.staticTexts["Waiting for Strava quota reset."].exists)
        XCUIDevice.shared.press(.home)
        app.activate()
        expect(state, value: "Waiting to retry")
        let cancel = app.buttons["Cancel sync"]
        reveal(cancel, in: app)
        cancel.tap()
        XCTAssertTrue(app.staticTexts["Could not update sync. Refresh status and try again."].waitForExistence(timeout: 5))
        expect(state, value: "Waiting to retry")
        cancel.tap()
        expect(state, value: "Sync cancelled")
        let resume = app.buttons["Resume unfinished activities"]
        reveal(resume, in: app)
        resume.tap()
        let interrupted = app.staticTexts["Connection interrupted. Server sync continues; reconnecting…"]
        XCTAssertTrue(interrupted.waitForExistence(timeout: 8))
        let refresh = app.buttons["Refresh sync status"]
        reveal(refresh, in: app)
        refresh.tap()
        expect(state, value: "Sync complete")
        app.tabBars.buttons["Activities"].tap()
        XCTAssertTrue(app.buttons["activity-44"].waitForExistence(timeout: 5))
    }
}
