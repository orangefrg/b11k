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
                Toggle("Choose a date range", isOn: $viewModel.syncDateRangeEnabled)
                    .accessibilityIdentifier("sync-date-range")
                    .disabled(viewModel.isSyncing)
                if viewModel.syncDateRangeEnabled {
                    DatePicker("Start", selection: $viewModel.startDate, displayedComponents: .date)
                        .accessibilityIdentifier("sync-start-date")
                    DatePicker("End", selection: $viewModel.endDate, displayedComponents: .date)
                        .accessibilityIdentifier("sync-end-date")
                    Text("Dates include the full end day, in UTC.").font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("Import all cycling history, newest first. Completed activities are skipped.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                Button(viewModel.isChangingSync ? "Contacting server…" : "Sync from Strava") {
                    focusedField = nil
                    Task { await viewModel.sync() }
                }
                .disabled(!viewModel.isAuthorized || viewModel.isSyncing || viewModel.isChangingSync)

                if let job = viewModel.syncJob {
                    Text(job.title).font(.headline).accessibilityIdentifier("sync-state")
                    if !job.message.isEmpty { Text(job.message).font(.caption) }
                    if !job.discoveryDone && job.shouldPoll {
                        ProgressView("Found \(job.summary.total) cycling activities")
                    }
                    LabeledContent("Imported", value: "\(job.summary.success)")
                    LabeledContent("Already complete", value: "\(job.summary.existing)")
                    LabeledContent("Failed", value: "\(job.summary.failed)")
                    LabeledContent("Waiting to import", value: "\(job.summary.pending ?? 0)")
                    if job.state == "waiting" {
                        LabeledContent("Next attempt") { Text(job.nextAttemptAt, style: .relative) }
                    }
                    if let last = job.lastSuccessfulAt {
                        LabeledContent("Last successful sync") { Text(last, format: .dateTime.month().day().hour().minute()) }
                    }
                    if job.isActive {
                        Text("You can close the app. Sync continues on the server.")
                            .font(.caption).foregroundStyle(.secondary)
                        Button("Cancel sync", role: .destructive) { Task { await viewModel.changeSync("cancel") } }
                            .disabled(viewModel.isChangingSync)
                    }
                    if job.canRetry {
                        Button(job.state == "cancelled" ? "Resume unfinished activities" : "Retry unfinished work") {
                            Task { await viewModel.changeSync("retry") }
                        }.disabled(viewModel.isChangingSync)
                    }
                    ForEach(job.failures) { failure in
                        Text("Activity \(failure.activityID): \(failure.message)")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                if !viewModel.syncConnectionMessage.isEmpty {
                    Text(viewModel.syncConnectionMessage).font(.caption)
                }
                Button("Refresh sync status") { Task { await viewModel.resumeSyncMonitoring() } }
                    .disabled(!viewModel.isAuthorized)
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
