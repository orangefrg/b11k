import SwiftUI

struct ProfileView: View {
    @ObservedObject var viewModel: AppViewModel

    var body: some View {
        List {
            if !viewModel.isAuthorized {
                ContentUnavailableView(
                    "Connect Strava",
                    systemImage: "person.crop.circle",
                    description: Text("Use Settings to connect to B11K.")
                )
            } else if let profile = viewModel.profile {
                let athlete = profile.athlete ?? viewModel.athlete

                Section {
                    HStack(spacing: 14) {
                        ProfileAvatarView(athlete: athlete)

                        VStack(alignment: .leading, spacing: 4) {
                            Text(profileName(athlete))
                                .font(.headline)
                            if let athlete {
                                Text("Strava ID \(athlete.id)")
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                    .padding(.vertical, 6)
                }

                Section("Summary") {
                    LabeledContent("Total activities", value: "\(profile.totalActivities)")
                    LabeledContent("Total bike distance", value: Formatters.distance(profile.totalBikeKM * 1000))
                    LabeledContent("Busiest month", value: periodValue(profile.bestMonth))
                    LabeledContent("Busiest year", value: periodValue(profile.bestYear))
                }

                Section("Bike Distance") {
                    if profile.hasRecordedRides {
                        ForEach(profile.bikeStats) { stat in
                            HStack(alignment: .firstTextBaseline) {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(stat.label)
                                        .font(.headline)
                                    Text(activityCount(stat.activities))
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                                Spacer()
                                Text(Formatters.distance(stat.distanceKM * 1000))
                                    .font(.headline)
                            }
                            .padding(.vertical, 3)
                        }
                    } else {
                        Text("No bike activities found in the local database.")
                            .foregroundStyle(.secondary)
                    }
                }

                Section("Heart Rate Zones") {
                    if profile.hrZones.isEmpty {
                        Text(hrZonesUnavailableText(profile.hrZonesError))
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(profile.hrZones) { zone in
                            LabeledContent(zone.label, value: zone.range)
                        }
                    }
                }

                Section {
                    Button(role: .destructive) {
                        Task { await viewModel.logout() }
                    } label: {
                        Label(viewModel.isLoggingOut ? "Logging out..." : "Logout", systemImage: "rectangle.portrait.and.arrow.right")
                    }
                    .disabled(viewModel.isBusy)
                }
            } else if viewModel.isLoadingProfile {
                Section {
                    ProgressView("Loading profile...")
                }
            } else {
                ContentUnavailableView(
                    "Profile Unavailable",
                    systemImage: "person.crop.circle",
                    description: Text("Profile data is not available.")
                )
            }
        }
        .task {
            if viewModel.isAuthorized && viewModel.profile == nil {
                await viewModel.loadProfile()
            }
        }
        .refreshable {
            await viewModel.loadProfile()
        }
        .overlay {
            if viewModel.isLoadingProfile && viewModel.profile != nil {
                ProgressView("Loading profile...")
            }
        }
    }

    private func profileName(_ athlete: Athlete?) -> String {
        guard let athlete else { return "Profile" }
        let name = "\(athlete.firstname) \(athlete.lastname)".trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? "Profile" : name
    }

    private func periodValue(_ period: ProfilePeriodStat) -> String {
        guard !period.label.isEmpty else { return "n/a" }
        return "\(period.label) · \(activityCount(period.activities))"
    }

    private func activityCount(_ count: Int) -> String {
        count == 1 ? "1 activity" : "\(count) activities"
    }

    private func hrZonesUnavailableText(_ error: String) -> String {
        if error.isEmpty {
            return "Heart rate zones are not available."
        }
        return "Heart rate zones are not available: \(error)"
    }
}

struct ProfileAvatarView: View {
    let athlete: Athlete?

    var body: some View {
        Group {
            if let url {
                AsyncImage(url: url) { phase in
                    switch phase {
                    case .success(let image):
                        image
                            .resizable()
                            .scaledToFill()
                    default:
                        fallback
                    }
                }
            } else {
                fallback
            }
        }
        .frame(width: 64, height: 64)
        .clipShape(Circle())
        .overlay {
            Circle()
                .stroke(Color.secondary.opacity(0.2), lineWidth: 1)
        }
    }

    private var url: URL? {
        guard let value = athlete?.profile.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else {
            return nil
        }
        return URL(string: value)
    }

    private var fallback: some View {
        ZStack {
            Circle()
                .fill(Color.secondary.opacity(0.12))
            Image(systemName: "person.crop.circle.fill")
                .font(.system(size: 42))
                .foregroundStyle(.secondary)
        }
    }
}
