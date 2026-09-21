import XCTest

final class B11kUITests: XCTestCase {
    @MainActor
    private func launchSettings() -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchArguments = ["--ui-testing"]
        app.launch()
        app.tabBars.buttons["Settings"].tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 5))
        return app
    }

    @MainActor
    func testSignedOutSyncIsDisabledAndDateRangeIsOptional() {
        let app = launchSettings()
        let sync = app.buttons["Sync from Strava"]
        app.swipeUp()
        XCTAssertTrue(sync.waitForExistence(timeout: 5))
        XCTAssertFalse(sync.isEnabled)
        XCTAssertTrue(app.staticTexts["Import all cycling history, newest first. Completed activities are skipped."].exists)
        let range = app.switches["sync-date-range"]
        // SwiftUI exposes the whole form row as a switch. Target the trailing
        // control instead of the label/background used to dismiss the keyboard.
        range.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
        XCTAssertEqual(range.value as? String, "1")
        XCTAssertTrue(app.descendants(matching: .any)["sync-start-date"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.descendants(matching: .any)["sync-end-date"].firstMatch.exists)
        range.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
        XCTAssertFalse(app.descendants(matching: .any)["sync-start-date"].firstMatch.exists)
    }

    @MainActor
    func testSaveBackendGuidesSignedOutUserWithoutMakingNetworkRequest() {
        let app = launchSettings()
        let url = app.textFields["https://api.example.com"]
        url.tap()
        url.typeText("https://test.example")
        app.toolbars.buttons["Done"].tap()
        app.buttons["Check Connection"].tap()
        XCTAssertTrue(app.alerts.staticTexts["Backend URL saved. Connect Strava next."].waitForExistence(timeout: 5))
    }

    @MainActor
    func testAllMainTabsRemainAvailableWhileSignedOut() {
        let app = launchSettings()
        for name in ["Activities", "Segments", "Discovered", "Profile", "Settings"] {
            app.tabBars.buttons[name].tap()
            XCTAssertTrue(app.navigationBars[name].waitForExistence(timeout: 5))
        }
    }
}
