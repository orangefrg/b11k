import SwiftUI

struct SettingsView: View {
    @ObservedObject var viewModel: AppViewModel
    @FocusState private var focusedField: SettingsField?

    private enum SettingsField: Hashable {
        case backendURL
    }

    var body: some View {
        Form {
            Section("Backend") {
                TextField("https://api.example.com", text: $viewModel.baseURLString)
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    .autocorrectionDisabled()
                    .focused($focusedField, equals: .backendURL)
                    .submitLabel(.done)
                    .onSubmit {
                        focusedField = nil
                    }

                Button("Check Connection") {
                    focusedField = nil
                    Task { await viewModel.loadMe() }
                }
            }

            Section("Strava") {
                if let athlete = viewModel.athlete {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("\(athlete.firstname) \(athlete.lastname)")
                            .font(.headline)
                        Text("Strava ID \(athlete.id)")
                            .foregroundStyle(.secondary)
                    }
                } else {
                    Text("Not connected")
                        .foregroundStyle(.secondary)
                }

                Button(viewModel.isAuthenticating ? "Connecting..." : "Connect Strava") {
                    focusedField = nil
                    Task { await viewModel.connectStrava() }
                }
                .disabled(viewModel.isBusy)

                if viewModel.isWaitingForBrowserAuth {
                    Button("Check Login") {
                        focusedField = nil
                        Task { await viewModel.checkBrowserLogin() }
                    }
                    .disabled(viewModel.isBusy)

                    Text("Finish Strava login in the browser, then return here.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Section("Sync") {
                DatePicker("Start", selection: $viewModel.startDate, displayedComponents: .date)
                DatePicker("End", selection: $viewModel.endDate, displayedComponents: .date)

                Button(viewModel.isSyncing ? "Syncing..." : "Sync from Strava") {
                    focusedField = nil
                    Task { await viewModel.sync() }
                }
                .disabled(!viewModel.isAuthorized || viewModel.isBusy)

                if let summary = viewModel.syncSummary {
                    LabeledContent("Total", value: "\(summary.total)")
                    LabeledContent("New", value: "\(summary.new)")
                    LabeledContent("Existing", value: "\(summary.existing)")
                    LabeledContent("Processed", value: "\(summary.success)")
                    LabeledContent("Failed", value: "\(summary.failed)")
                }
            }

            Section("Library") {
                Button("Refresh Activities") {
                    focusedField = nil
                    Task { await viewModel.loadActivities(reset: true) }
                }
                .disabled(!viewModel.isAuthorized || viewModel.isBusy)

                LabeledContent("Stored activities", value: "\(viewModel.activityCount)")
                LabeledContent("Loaded on device", value: "\(viewModel.activities.count)")
            }

            if !viewModel.logLines.isEmpty {
                Section("Log") {
                    ForEach(viewModel.logLines, id: \.self) { line in
                        Text(line)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .scrollDismissesKeyboard(.interactively)
        .background {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture {
                    focusedField = nil
                }
        }
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("Done") {
                    focusedField = nil
                }
            }
        }
    }
}
