//
//  B11kApp.swift
//  B11k
//
//  Created in 2026.
//

import SwiftUI

@main
struct B11kApp: App {
    @StateObject private var viewModel = B11kApp.makeViewModel()
    var body: some Scene {
        WindowGroup {
            ContentView(viewModel: viewModel)
        }
    }

    private static func makeViewModel() -> AppViewModel {
        #if DEBUG
        // UI automation uses isolated preferences and no backend or Keychain.
        // Both the fixture transport and this launch path are absent in Release.
        if ProcessInfo.processInfo.arguments.contains("--ui-testing") || ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil {
            let defaults = UserDefaults(suiteName: "B11k.UITests")!
            defaults.removePersistentDomain(forName: "B11k.UITests")
            if ProcessInfo.processInfo.arguments.contains("--ui-testing-authenticated") {
                defaults.set("https://ui-test.invalid", forKey: "b11k.baseURL")
                let backend = UITestBackend()
                return AppViewModel(defaults: defaults, loadToken: { "ui-test-token" }, saveToken: { _ in },
                                    transport: { try await backend.respond(to: $0) })
            }
            return AppViewModel(defaults: defaults, loadToken: { "" }, saveToken: { _ in },
                                transport: { _ in throw URLError(.notConnectedToInternet) })
        }
        #endif
        return AppViewModel()
    }
}
