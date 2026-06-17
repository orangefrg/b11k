//
//  ContentView.swift
//  B11k
//
//  Created in 2026.
//

import AuthenticationServices
import Combine
import MapKit
import SwiftUI
import UIKit

struct ContentView: View {
    @StateObject private var viewModel = AppViewModel()

    var body: some View {
        TabView {
            NavigationStack {
                ActivitiesView(viewModel: viewModel)
                    .navigationTitle("Activities")
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button {
                                Task { await viewModel.loadActivities(reset: true) }
                            } label: {
                                Image(systemName: "arrow.clockwise")
                            }
                            .disabled(!viewModel.isAuthorized || viewModel.isBusy)
                        }
                    }
            }
            .tabItem {
                Label("Activities", systemImage: "list.bullet")
            }

            NavigationStack {
                SegmentsView(viewModel: viewModel)
                    .navigationTitle("Segments")
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button {
                                Task { await viewModel.loadSegments() }
                            } label: {
                                Image(systemName: "arrow.clockwise")
                            }
                            .disabled(!viewModel.isAuthorized || viewModel.isBusy)
                        }
                    }
            }
            .tabItem {
                Label("Segments", systemImage: "point.topleft.down.curvedto.point.bottomright.up")
            }

            NavigationStack {
                DiscoveredView(viewModel: viewModel)
                    .navigationTitle("Discovered")
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button {
                                Task { await viewModel.loadDiscoveredStatus() }
                            } label: {
                                Image(systemName: "arrow.clockwise")
                            }
                            .disabled(!viewModel.isAuthorized || viewModel.isBusy)
                        }
                    }
            }
            .tabItem {
                Label("Discovered", systemImage: "map")
            }

            NavigationStack {
                ProfileView(viewModel: viewModel)
                    .navigationTitle("Profile")
                    .toolbar {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button {
                                Task { await viewModel.loadProfile() }
                            } label: {
                                Image(systemName: "arrow.clockwise")
                            }
                            .disabled(!viewModel.isAuthorized || viewModel.isBusy)
                        }
                    }
            }
            .tabItem {
                Label("Profile", systemImage: "person.crop.circle")
            }

            NavigationStack {
                SettingsView(viewModel: viewModel)
                    .navigationTitle("Settings")
                    .toolbar {
                        if viewModel.isBusy {
                            ProgressView()
                        }
                    }
            }
            .tabItem {
                Label("Settings", systemImage: "gearshape")
            }
        }
        .onOpenURL { url in
            Task { await viewModel.handleAuthCallback(url) }
        }
        .task {
            await viewModel.restoreSession()
        }
        .alert("B11K", isPresented: $viewModel.showingMessage) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(viewModel.message)
        }
    }
}
struct ActivitiesView: View {
    @ObservedObject var viewModel: AppViewModel
    @FocusState private var isSearchFocused: Bool

    var body: some View {
        List {
            if !viewModel.isAuthorized {
                ContentUnavailableView(
                    "Connect Strava",
                    systemImage: "figure.outdoor.cycle",
                    description: Text("Use Settings to connect to B11K.")
                )
            } else {
                Section("Search") {
                    TextField("Name, place, gear", text: $viewModel.activitySearch)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($isSearchFocused)
                        .submitLabel(.search)
                        .onSubmit {
                            isSearchFocused = false
                            Task { await viewModel.loadActivities(reset: true) }
                        }

                    Button("Search Activities") {
                        isSearchFocused = false
                        Task { await viewModel.loadActivities(reset: true) }
                    }
                    .disabled(viewModel.isBusy)
                }

                if viewModel.activities.isEmpty && !viewModel.isLoadingActivities {
                    ContentUnavailableView(
                        "No Activities",
                        systemImage: "list.bullet.rectangle",
                        description: Text("Sync from Strava, refresh this list, or change the search.")
                    )
                } else if let stats = viewModel.activityStats {
                    Section("Summary") {
                        LabeledContent("Loaded", value: "\(stats.count)")
                        LabeledContent("Total Matches", value: "\(viewModel.activityCount)")
                        LabeledContent("Distance", value: Formatters.distance(stats.distance))
                        LabeledContent("Elevation", value: Formatters.elevation(stats.elevation))
                        LabeledContent("Moving Time", value: Formatters.duration(stats.movingTime))
                    }
                    Section("Recent") {
                        ForEach(viewModel.activities) { activity in
                            NavigationLink {
                                ActivityDetailView(viewModel: viewModel, activity: activity)
                            } label: {
                                ActivityRow(activity: activity)
                            }
                        }

                        if viewModel.hasMoreActivities {
                            Button(viewModel.isLoadingActivities ? "Loading..." : "Load More") {
                                Task { await viewModel.loadMoreActivities() }
                            }
                            .disabled(viewModel.isBusy)
                        }
                    }
                }
            }
        }
        .overlay {
            if viewModel.isLoadingActivities {
                ProgressView("Loading activities...")
            }
        }
        .refreshable {
            await viewModel.loadActivities(reset: true)
        }
        .scrollDismissesKeyboard(.interactively)
        .background {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture {
                    isSearchFocused = false
                }
        }
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("Done") {
                    isSearchFocused = false
                }
            }
        }
    }
}

struct ActivityRow: View {
    let activity: Activity

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(activity.name)
                    .font(.headline)
                    .lineLimit(1)
                Spacer()
                Text(Formatters.distance(activity.distance))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 8) {
                Text(activity.displayType)
                Text(Formatters.date(activity.startDate))
                Text(Formatters.duration(activity.movingTime))
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }
}

struct ActivityDetailView: View {
    @ObservedObject var viewModel: AppViewModel
    let activity: Activity
    @State private var routeSnapshot: RouteSnapshot?
    @State private var isLoadingRoute = false
    @State private var paintMetric: RoutePaintMetric = .none
    @State private var chartMetric: RoutePaintMetric = .altitude
    @State private var isShowingSegmentCreator = false

    var body: some View {
        Form {
            Section {
                Text(activity.name)
                    .font(.title2)
                    .fontWeight(.semibold)
                LabeledContent("Sport", value: activity.displayType)
                LabeledContent("Date", value: Formatters.longDate(activity.startDate))
                if let location = activity.locationText {
                    LabeledContent("Location", value: location)
                }
            }

            Section("Map") {
                if let routeSnapshot, routeSnapshot.points.count >= 2 {
                    Picker("Paint", selection: $paintMetric) {
                        ForEach(RoutePaintMetric.paintMetrics) { metric in
                            Text(metric.title).tag(metric)
                        }
                    }
                    .pickerStyle(.menu)

                    VStack {
                        ActivityRouteMap(points: routeSnapshot.points, paintMetric: paintMetric)
                            .frame(height: 260)
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                    }
                    .frame(maxWidth: .infinity, minHeight: 260)
                    LabeledContent("Route source", value: routeSnapshot.sourceLabel)
                    LabeledContent("Route points", value: "\(routeSnapshot.count)")
                    if paintMetric != .none && !routeSnapshot.hasData(for: paintMetric) {
                        Text("\(paintMetric.title) samples are not stored for this activity.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                } else if isLoadingRoute {
                    ProgressView("Loading route...")
                } else {
                    Text("No route points stored for this activity.")
                        .foregroundStyle(.secondary)
                    if let routeSnapshot {
                        LabeledContent("Route source", value: routeSnapshot.sourceLabel)
                        LabeledContent("Route points", value: "\(routeSnapshot.count)")
                    }
                }
            }

            if let routeSnapshot, routeSnapshot.source == "point_samples" {
                Section("Segments") {
                    Button {
                        isShowingSegmentCreator = true
                    } label: {
                        Label("Create Segment", systemImage: "point.topleft.down.curvedto.point.bottomright.up")
                    }
                    .disabled(routeSnapshot.points.count < 2 || viewModel.isBusy)
                }

                Section("Sample Data") {
                    ForEach(RoutePaintMetric.chartMetrics) { metric in
                        LabeledContent(metric.title, value: "\(routeSnapshot.sampleCount(for: metric))")
                    }
                }
            }

            Section("Effort") {
                LabeledContent("Distance", value: Formatters.distance(activity.distance))
                LabeledContent("Moving Time", value: Formatters.duration(activity.movingTime))
                LabeledContent("Elapsed Time", value: Formatters.duration(activity.elapsedTime))
                LabeledContent("Elevation", value: Formatters.elevation(activity.totalElevationGain))
                LabeledContent("Average Speed", value: Formatters.speed(activity.averageSpeed))
                LabeledContent("Max Speed", value: Formatters.speed(activity.maxSpeed))
                if let routeSnapshot {
                    if let topSpeed = routeSnapshot.maxSpeed {
                        LabeledContent("Top Speed", value: Formatters.speed(topSpeed))
                    }
                    if let topCadence = routeSnapshot.maxCadence {
                        LabeledContent("Top Cadence", value: "\(Formatters.wholeNumber(topCadence)) rpm")
                    }
                    if let topGrade = routeSnapshot.maxGrade {
                        LabeledContent("Max Slope", value: "\(Formatters.number(topGrade))%")
                    }
                    if let maxElevation = routeSnapshot.maxElevation {
                        LabeledContent("Top Elevation", value: Formatters.elevation(maxElevation))
                    }
                }
            }

            Section("Metrics") {
                metricRow("Average HR", value: activity.averageHeartrate, suffix: "bpm")
                metricRow("Max HR", value: activity.maxHeartrate, suffix: "bpm")
                metricRow("Average Watts", value: activity.averageWatts, suffix: "W")
                metricRow("Max Watts", value: activity.maxWatts, suffix: "W")
                metricRow("Cadence", value: activity.averageCadence, suffix: "rpm")
                metricRow("Work", value: activity.kilojoules, suffix: "kJ")
                metricRow("Suffer Score", value: activity.sufferScore, suffix: "")
            }

            if activity.gearName != nil || !activity.gearID.isEmpty {
                Section("Gear") {
                    if let gearName = activity.gearName {
                        LabeledContent("Name", value: gearName)
                    }
                    if !activity.gearID.isEmpty {
                        LabeledContent("ID", value: activity.gearID)
                    }
                }
            }

            if let routeSnapshot, routeSnapshot.hasChartData {
                Section("Diagrams") {
                    Picker("Metric", selection: $chartMetric) {
                        ForEach(routeSnapshot.chartMetrics) { metric in
                            Text(metric.title).tag(metric)
                        }
                    }
                    .pickerStyle(.segmented)
                    .onAppear {
                        if !routeSnapshot.hasData(for: chartMetric) {
                            chartMetric = routeSnapshot.chartMetrics.first ?? .altitude
                        }
                    }
                    .onChange(of: routeSnapshot.chartMetrics) { _, metrics in
                        if !metrics.contains(chartMetric) {
                            chartMetric = metrics.first ?? .altitude
                        }
                    }

                    RouteMetricChart(points: routeSnapshot.points, metric: chartMetric)
                }
            }
        }
        .navigationTitle("Activity")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: activity.id) {
            await loadRoute()
        }
        .sheet(isPresented: $isShowingSegmentCreator) {
            if let routeSnapshot {
                SegmentCreateView(viewModel: viewModel, activity: activity, routeSnapshot: routeSnapshot)
            }
        }
    }

    @ViewBuilder
    private func metricRow(_ title: String, value: Double, suffix: String) -> some View {
        if value > 0 {
            LabeledContent(title, value: suffix.isEmpty ? Formatters.number(value) : "\(Formatters.number(value)) \(suffix)")
        }
    }

    private func loadRoute() async {
        isLoadingRoute = true
        defer { isLoadingRoute = false }
        routeSnapshot = await viewModel.loadRoute(activityID: activity.id)
    }
}

struct ActivityRouteMap: UIViewRepresentable {
    let points: [RoutePoint]
    let paintMetric: RoutePaintMetric

    func makeUIView(context: Context) -> RouteMapView {
        let mapView = RouteMapView()
        mapView.delegate = context.coordinator
        mapView.pointOfInterestFilter = .excludingAll
        mapView.showsCompass = false
        mapView.isRotateEnabled = false
        let coordinator = context.coordinator
        mapView.onUsableBounds = { [weak mapView, weak coordinator] in
            guard let mapView, let coordinator else { return }
            coordinator.renderIfNeeded(on: mapView)
        }
        return mapView
    }

    func updateUIView(_ mapView: RouteMapView, context: Context) {
        context.coordinator.update(points: points, paintMetric: paintMetric)
        context.coordinator.renderIfNeeded(on: mapView)
    }

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    final class Coordinator: NSObject, MKMapViewDelegate {
        private var points: [RoutePoint] = []
        private var paintMetric: RoutePaintMetric = .none
        private var renderedSignature = ""

        func update(points: [RoutePoint], paintMetric: RoutePaintMetric) {
            self.points = points
            self.paintMetric = paintMetric
        }

        func renderIfNeeded(on mapView: MKMapView) {
            guard mapView.bounds.width > 1, mapView.bounds.height > 1 else {
                return
            }

            let coordinates = validCoordinates(from: points)
            guard coordinates.count >= 2 else {
                mapView.removeOverlays(mapView.overlays)
                return
            }

            let signature = routeSignature(for: points, metric: paintMetric)
            guard signature != renderedSignature || mapView.overlays.isEmpty else {
                return
            }

            mapView.removeOverlays(mapView.overlays)
            if paintMetric == .none {
                let polyline = MKPolyline(coordinates: coordinates, count: coordinates.count)
                mapView.addOverlay(polyline)
            } else {
                let segments = coloredSegments(from: points, metric: paintMetric)
                if segments.isEmpty {
                    let polyline = MKPolyline(coordinates: coordinates, count: coordinates.count)
                    mapView.addOverlay(polyline)
                } else {
                    mapView.addOverlays(segments)
                }
            }

            let routeBounds = MKPolyline(coordinates: coordinates, count: coordinates.count).boundingMapRect
            mapView.setVisibleMapRect(
                routeBounds,
                edgePadding: UIEdgeInsets(top: 24, left: 24, bottom: 24, right: 24),
                animated: false
            )
            renderedSignature = signature
        }

        func mapView(_ mapView: MKMapView, rendererFor overlay: MKOverlay) -> MKOverlayRenderer {
            if let polyline = overlay as? ColoredPolyline {
                let renderer = MKPolylineRenderer(polyline: polyline)
                renderer.strokeColor = polyline.strokeColor
                renderer.lineWidth = 4
                renderer.lineJoin = .round
                renderer.lineCap = .round
                return renderer
            }
            guard let polyline = overlay as? MKPolyline else {
                return MKOverlayRenderer(overlay: overlay)
            }
            let renderer = MKPolylineRenderer(polyline: polyline)
            renderer.strokeColor = UIColor.systemOrange
            renderer.lineWidth = 4
            renderer.lineJoin = .round
            renderer.lineCap = .round
            return renderer
        }

        private func validCoordinates(from points: [RoutePoint]) -> [CLLocationCoordinate2D] {
            points
                .filter { (-90...90).contains($0.lat) && (-180...180).contains($0.lng) }
                .map { CLLocationCoordinate2D(latitude: $0.lat, longitude: $0.lng) }
        }

        private func coloredSegments(from points: [RoutePoint], metric: RoutePaintMetric) -> [ColoredPolyline] {
            let validPoints = points.filter { (-90...90).contains($0.lat) && (-180...180).contains($0.lng) }
            guard validPoints.count >= 2 else { return [] }
            let values = validPoints.compactMap { metric.value(from: $0) }
            guard let minValue = values.min(), let maxValue = values.max(), minValue < maxValue else { return [] }

            let step = max(1, validPoints.count / 180)
            var segments: [ColoredPolyline] = []
            var previous = validPoints[0]
            for index in stride(from: step, to: validPoints.count, by: step) {
                let current = validPoints[index]
                if let value = metric.value(from: current) {
                    var coordinates = [
                        CLLocationCoordinate2D(latitude: previous.lat, longitude: previous.lng),
                        CLLocationCoordinate2D(latitude: current.lat, longitude: current.lng)
                    ]
                    let polyline = ColoredPolyline(coordinates: &coordinates, count: coordinates.count)
                    polyline.strokeColor = metric.color(for: value, min: minValue, max: maxValue)
                    segments.append(polyline)
                }
                previous = current
            }
            return segments
        }

        private func routeSignature(for points: [RoutePoint], metric: RoutePaintMetric) -> String {
            guard let first = points.first, let last = points.last else {
                return metric.rawValue
            }
            return "\(metric.rawValue)-\(points.count)-\(first.index)-\(last.index)-\(first.lat)-\(first.lng)-\(last.lat)-\(last.lng)"
        }
    }
}

final class RouteMapView: MKMapView {
    var onUsableBounds: (() -> Void)?

    override func layoutSubviews() {
        super.layoutSubviews()
        if bounds.width > 1, bounds.height > 1 {
            onUsableBounds?()
        }
    }
}

final class ColoredPolyline: MKPolyline {
    var strokeColor: UIColor = .systemOrange
}

struct RouteMetricChart: View {
    let points: [RoutePoint]
    let metric: RoutePaintMetric

    private var chartSamples: [RouteChartSample] {
        let allSamples: [RouteChartSample] = points.compactMap { point in
            guard let value = metric.value(from: point) else { return nil }
            return RouteChartSample(id: point.index, x: point.chartX, value: value)
        }
        return allSamples.downsampled(maxCount: 320)
    }

    var body: some View {
        let samples = chartSamples
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(metric.title)
                    .font(.headline)
                Spacer()
                if let maxValue = samples.map(\.value).max() {
                    Text("Top \(metric.format(maxValue))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            RouteLineChart(samples: samples, metric: metric)
            .frame(height: 150)
        }
        .padding(.vertical, 6)
    }
}

struct RouteLineChart: View {
    let samples: [RouteChartSample]
    let metric: RoutePaintMetric

    var body: some View {
        let domain = ChartDomain(samples: samples, metric: metric)
        ZStack(alignment: .trailing) {
            Canvas { context, size in
                drawGrid(in: &context, size: size, domain: domain)
                drawLine(in: &context, size: size, domain: domain)
            }
            VStack {
                Text(metric.format(domain.maxValue))
                Spacer()
                Text(metric.format(domain.midValue))
                Spacer()
                Text(metric.format(domain.minValue))
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }

    private func drawGrid(in context: inout GraphicsContext, size: CGSize, domain: ChartDomain) {
        guard size.width > 1, size.height > 1 else { return }
        let style = StrokeStyle(lineWidth: 0.6)
        for fraction in [0.0, 0.5, 1.0] {
            let y = size.height * fraction
            var path = Path()
            path.move(to: CGPoint(x: 0, y: y))
            path.addLine(to: CGPoint(x: size.width - 58, y: y))
            context.stroke(path, with: .color(.secondary.opacity(0.28)), style: style)
        }
    }

    private func drawLine(in context: inout GraphicsContext, size: CGSize, domain: ChartDomain) {
        guard samples.count >= 2, domain.xMax > domain.xMin, domain.maxValue > domain.minValue else { return }
        let plotWidth = max(1, size.width - 58)
        let plotHeight = max(1, size.height)
        var path = Path()
        for (index, sample) in samples.enumerated() {
            let xRatio = (sample.x - domain.xMin) / (domain.xMax - domain.xMin)
            let yRatio = (sample.value - domain.minValue) / (domain.maxValue - domain.minValue)
            let point = CGPoint(
                x: plotWidth * min(1, max(0, xRatio)),
                y: plotHeight * (1 - min(1, max(0, yRatio)))
            )
            if index == 0 {
                path.move(to: point)
            } else {
                path.addLine(to: point)
            }
        }
        context.stroke(path, with: .color(metric.chartColor), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round))
    }
}

struct RouteChartSample: Identifiable {
    let id: Int
    let x: Double
    let value: Double
}

struct ChartDomain {
    let xMin: Double
    let xMax: Double
    let minValue: Double
    let maxValue: Double

    init(samples: [RouteChartSample], metric: RoutePaintMetric) {
        let values = samples.map(\.value)
        xMin = samples.map(\.x).min() ?? 0
        xMax = samples.map(\.x).max() ?? 1
        let rawMin = values.min() ?? 0
        let rawMax = values.max() ?? 1
        if metric == .grade && rawMin < 0 {
            minValue = floor(rawMin / 5) * 5
            maxValue = max(5, ceil(rawMax / 5) * 5)
        } else {
            minValue = 0
            maxValue = Self.niceUpperBound(rawMax)
        }
    }

    var midValue: Double {
        (minValue + maxValue) / 2
    }

    private static func niceUpperBound(_ value: Double) -> Double {
        guard value > 0 else { return 1 }
        let magnitude = pow(10, floor(log10(value)))
        let normalized = value / magnitude
        let nice: Double
        switch normalized {
        case ...1:
            nice = 1
        case ...2:
            nice = 2
        case ...5:
            nice = 5
        default:
            nice = 10
        }
        return nice * magnitude
    }
}

extension Array {
    func downsampled(maxCount: Int) -> [Element] {
        guard maxCount > 0, count > maxCount else { return self }
        let step = Double(count - 1) / Double(maxCount - 1)
        return (0..<maxCount).map { index in
            self[Int((Double(index) * step).rounded())]
        }
    }
}

@MainActor
final class AppViewModel: NSObject, ObservableObject, ASWebAuthenticationPresentationContextProviding {
    @AppStorage("b11k.baseURL") var baseURLString = ""
    @Published private var sessionToken = ""

    @Published var athlete: Athlete?
    @Published var activityCount = 0
    @Published var activities: [Activity] = []
    @Published var activitySearch = ""
    @Published var activityPage = 1
    @Published var hasMoreActivities = false
    @Published var syncSummary: SyncSummary?
    @Published var segments: [SegmentSummary] = []
    @Published var segmentSearch = ""
    @Published var profile: ProfileSummary?
    @Published var discoveredStatus: DiscoveredStatus?
    @Published var discoveredMapSnapshot: DiscoveredMapSnapshot?
    @Published var logLines: [String] = []
    @Published var message = ""
    @Published var showingMessage = false
    @Published var isAuthenticating = false
    @Published var isSyncing = false
    @Published var isRebuildingDatabase = false
    @Published var isLoadingActivities = false
    @Published var isLoadingSegments = false
    @Published var isLoadingSegmentEfforts = false
    @Published var isLoadingSegmentEffortDetail = false
    @Published var isLoadingDiscovered = false
    @Published var isLoadingDiscoveredMap = false
    @Published var isRebuildingDiscovered = false
    @Published var isLoadingProfile = false
    @Published var isLoggingOut = false
    @Published var isMutatingSegment = false
    @Published var isWaitingForBrowserAuth = false
    @Published var rebuildConfirmation = ""
    @Published var rebuildStorage: RebuildStorage?

    @Published var startDate: Date = Calendar.current.date(byAdding: .month, value: -1, to: Date()) ?? Date()
    @Published var endDate: Date = Date()

    private var pendingState: String?
    private var authSession: ASWebAuthenticationSession?

    override init() {
        let keychainToken = KeychainSessionStore.load()
        let legacyToken = UserDefaults.standard.string(forKey: "b11k.sessionToken") ?? ""
        super.init()

        if !keychainToken.isEmpty {
            sessionToken = keychainToken
        } else if !legacyToken.isEmpty {
            sessionToken = legacyToken
            KeychainSessionStore.save(legacyToken)
            UserDefaults.standard.removeObject(forKey: "b11k.sessionToken")
        }
    }

    var isAuthorized: Bool {
        !sessionToken.isEmpty
    }

    var isBusy: Bool {
        isAuthenticating || isSyncing || isRebuildingDatabase || isLoadingActivities || isLoadingSegments || isLoadingSegmentEfforts || isLoadingSegmentEffortDetail || isLoadingDiscovered || isLoadingDiscoveredMap || isRebuildingDiscovered || isLoadingProfile || isLoggingOut || isMutatingSegment
    }

    var canRebuildDatabase: Bool {
        isAuthorized && !isBusy && rebuildConfirmation.trimmingCharacters(in: .whitespacesAndNewlines) == "REBUILD"
    }

    var activityStats: ActivityStats? {
        guard !activities.isEmpty else { return nil }
        return ActivityStats(
            count: activities.count,
            distance: activities.reduce(0) { $0 + $1.distance },
            elevation: activities.reduce(0) { $0 + $1.totalElevationGain },
            movingTime: activities.reduce(0) { $0 + $1.movingTime }
        )
    }

    func restoreSession() async {
        guard isAuthorized, athlete == nil else { return }
        await loadMe()
    }

    func connectStrava() async {
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }

        isAuthenticating = true
        defer { isAuthenticating = false }

        do {
            let start: AuthStartResponse = try await request(baseURL.appending(path: "/api/mobile/auth/start"))
            pendingState = start.state
            isWaitingForBrowserAuth = false

            if let appURL = URL(string: start.appAuthURL), UIApplication.shared.canOpenURL(appURL) {
                isWaitingForBrowserAuth = true
                _ = await UIApplication.shared.open(appURL)
                return
            }

            guard let webURL = URL(string: start.webAuthURL) else {
                show("Backend returned an invalid Strava URL.")
                return
            }

            if let redirectURL = URL(string: start.redirectURI),
               let redirectScheme = redirectURL.scheme?.lowercased(),
               let redirectHost = redirectURL.host,
               redirectScheme == "http" || redirectScheme == "https" {
                guard Self.isBackendSchemeAllowed(scheme: redirectScheme, host: redirectHost) else {
                    show("Backend OAuth redirect must use HTTPS.")
                    return
                }
                isWaitingForBrowserAuth = true
                _ = await UIApplication.shared.open(webURL)
                return
            }

            authSession = ASWebAuthenticationSession(url: webURL, callbackURLScheme: "b11k") { [weak self] callbackURL, error in
                guard let self else { return }
                Task { @MainActor in
                    if let callbackURL {
                        await self.handleAuthCallback(callbackURL)
                    } else if let error {
                        self.show(error.localizedDescription)
                    }
                    self.authSession = nil
                }
            }
            authSession?.presentationContextProvider = self
            authSession?.prefersEphemeralWebBrowserSession = false
            if authSession?.start() != true {
                show("Could not start Strava login.")
            }
        } catch {
            show(error.localizedDescription)
        }
    }

    func handleAuthCallback(_ url: URL) async {
        guard url.scheme == "b11k" else { return }
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }

        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let items = components?.queryItems ?? []
        guard let code = items.first(where: { $0.name == "code" })?.value else {
            let error = items.first(where: { $0.name == "error" })?.value ?? "Missing authorization code."
            show(error)
            return
        }
        let state = items.first(where: { $0.name == "state" })?.value ?? pendingState ?? ""
        let scope = items.first(where: { $0.name == "scope" })?.value ?? ""

        do {
            let body = AuthExchangeRequest(code: code, state: state, scope: scope)
            let response: AuthExchangeResponse = try await request(
                baseURL.appending(path: "/api/mobile/auth/exchange"),
                method: "POST",
                body: body
            )
            setSessionToken(response.sessionToken)
            athlete = response.athlete
            pendingState = nil
            isWaitingForBrowserAuth = false
            logLines.insert("Connected to Strava.", at: 0)
            profile = nil
            await loadActivities(reset: true)
        } catch {
            show(error.localizedDescription)
        }
    }

    func checkBrowserLogin() async {
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }
        guard let pendingState else {
            show("Start Strava login first.")
            return
        }

        do {
            var components = URLComponents(url: baseURL.appending(path: "/api/mobile/auth/session"), resolvingAgainstBaseURL: false)
            components?.queryItems = [URLQueryItem(name: "state", value: pendingState)]
            guard let url = components?.url else {
                show("Could not build login check URL.")
                return
            }

            let response: AuthSessionResponse = try await request(url)
            switch response.status {
            case "pending":
                show("Still waiting for Strava callback. Finish login in the browser, then try again.")
            case "error":
                isWaitingForBrowserAuth = false
                self.pendingState = nil
                show(response.error ?? "Strava login failed.")
            case "ready":
                guard let token = response.sessionToken, let athlete = response.athlete else {
                    show("Backend returned an incomplete session.")
                    return
                }
                setSessionToken(token)
                self.athlete = athlete
                isWaitingForBrowserAuth = false
                self.pendingState = nil
                logLines.insert("Connected to Strava.", at: 0)
                profile = nil
                await loadActivities(reset: true)
            default:
                show("Unexpected login status: \(response.status)")
            }
        } catch {
            show(error.localizedDescription)
        }
    }

    func loadMe() async {
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }
        guard isAuthorized else {
            show("Backend URL saved. Connect Strava next.")
            return
        }

        do {
            let response: MeResponse = try await request(baseURL.appending(path: "/api/mobile/me"), authorized: true)
            athlete = response.athlete
            await loadActivities(reset: true)
        } catch {
            handleRequestError(error)
        }
    }

    func sync() async {
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }
        isSyncing = true
        syncSummary = nil
        logLines = ["Sync started..."]
        defer { isSyncing = false }

        do {
            var components = URLComponents(url: baseURL.appending(path: "/api/mobile/sync"), resolvingAgainstBaseURL: false)
            components?.queryItems = [
                URLQueryItem(name: "start", value: Self.dateFormatter.string(from: startDate)),
                URLQueryItem(name: "end", value: Self.dateFormatter.string(from: endDate))
            ]
            guard let url = components?.url else {
                show("Could not build sync URL.")
                return
            }
            let response: SyncResponse = try await request(url, method: "POST", authorized: true)
            syncSummary = response.summary
            logLines = response.logs.isEmpty ? ["Sync complete."] : response.logs
            profile = nil
            await loadActivities(reset: true)
            await loadSegments()
            await loadDiscoveredStatus()
        } catch {
            handleRequestError(error)
        }
    }

    func rebuildDatabaseAndSync() async {
        guard let baseURL else {
            show("Enter a valid backend URL.")
            return
        }
        guard rebuildConfirmation.trimmingCharacters(in: .whitespacesAndNewlines) == "REBUILD" else {
            show("Type REBUILD before running the live test.")
            return
        }

        isRebuildingDatabase = true
        syncSummary = nil
        rebuildStorage = nil
        logLines = ["Rebuild test started..."]
        defer { isRebuildingDatabase = false }

        do {
            var components = URLComponents(url: baseURL.appending(path: "/api/mobile/dev/rebuild-sync"), resolvingAgainstBaseURL: false)
            components?.queryItems = [
                URLQueryItem(name: "start", value: Self.dateFormatter.string(from: startDate)),
                URLQueryItem(name: "end", value: Self.dateFormatter.string(from: endDate))
            ]
            guard let url = components?.url else {
                show("Could not build rebuild URL.")
                return
            }
            let body = RebuildSyncRequest(confirmation: "REBUILD")
            let response: RebuildSyncResponse = try await request(url, method: "POST", body: body, authorized: true)
            syncSummary = response.summary
            rebuildStorage = response.storage
            logLines = response.logs.isEmpty ? ["Rebuild test complete."] : response.logs
            rebuildConfirmation = ""
            profile = nil
            await loadActivities(reset: true)
            await loadSegments()
            await loadDiscoveredStatus()
        } catch {
            handleRequestError(error)
        }
    }

    func loadActivities(reset: Bool = true) async {
        guard let baseURL, isAuthorized else { return }
        isLoadingActivities = true
        defer { isLoadingActivities = false }

        do {
            let page = reset ? 1 : activityPage + 1
            var components = URLComponents(url: baseURL.appending(path: "/api/mobile/activities"), resolvingAgainstBaseURL: false)
            components?.queryItems = [
                URLQueryItem(name: "page", value: "\(page)"),
                URLQueryItem(name: "per_page", value: "100")
            ]
            let trimmedSearch = activitySearch.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmedSearch.isEmpty {
                components?.queryItems?.append(URLQueryItem(name: "q", value: trimmedSearch))
            }
            guard let url = components?.url else {
                show("Could not build activities URL.")
                return
            }
            let response: ActivitiesResponse = try await request(url, authorized: true)
            activityCount = response.count
            activityPage = response.page
            hasMoreActivities = response.hasMore
            if reset {
                activities = response.activities
            } else {
                activities.append(contentsOf: response.activities)
            }
        } catch {
            handleRequestError(error)
        }
    }

    func loadMoreActivities() async {
        guard hasMoreActivities, !isLoadingActivities else { return }
        await loadActivities(reset: false)
    }

    func loadRoute(activityID: Int64) async -> RouteSnapshot? {
        guard isAuthorized else { return nil }
        do {
            guard let url = mobileURL(path: "/api/mobile/activities/\(activityID)/route") else {
                return nil
            }
            let response: ActivityRouteResponse = try await request(url, authorized: true)
            return RouteSnapshot(source: response.source, count: response.count, points: response.points)
        } catch {
            if case AppError.http(let statusCode, _) = error, statusCode == 401 {
                handleRequestError(error)
            }
            return nil
        }
    }

    func loadSegments() async {
        guard isAuthorized else { return }
        isLoadingSegments = true
        defer { isLoadingSegments = false }

        do {
            var queryItems: [URLQueryItem] = []
            let trimmedSearch = segmentSearch.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmedSearch.isEmpty {
                queryItems.append(URLQueryItem(name: "q", value: trimmedSearch))
            }
            guard let url = mobileURL(path: "/api/mobile/segments", queryItems: queryItems) else {
                show("Could not build segments URL.")
                return
            }
            let response: SegmentsResponse = try await request(url, authorized: true)
            segments = response.segments
        } catch {
            handleRequestError(error)
        }
    }

    func loadSegment(id: Int64) async -> SegmentDetail? {
        guard isAuthorized else { return nil }
        do {
            guard let url = mobileURL(path: "/api/mobile/segments/\(id)") else {
                return nil
            }
            let response: SegmentDetailResponse = try await request(url, authorized: true)
            return response.segment
        } catch {
            if case AppError.http(let statusCode, _) = error, statusCode == 401 {
                handleRequestError(error)
            }
            return nil
        }
    }

    func loadSegmentEfforts(segmentID: Int64, tolerance: Double, sort: SegmentEffortSort, refresh: Bool = false) async -> SegmentEffortsResponse? {
        guard isAuthorized else { return nil }
        isLoadingSegmentEfforts = true
        defer { isLoadingSegmentEfforts = false }

        do {
            var queryItems = [
                URLQueryItem(name: "tolerance", value: Self.queryNumber(tolerance)),
                URLQueryItem(name: "sort", value: sort.rawValue)
            ]
            if refresh {
                queryItems.append(URLQueryItem(name: "refresh", value: "true"))
            }
            guard let url = mobileURL(path: "/api/mobile/segments/\(segmentID)/activities", queryItems: queryItems) else {
                show("Could not build segment efforts URL.")
                return nil
            }
            return try await request(url, authorized: true)
        } catch {
            handleRequestError(error)
            return nil
        }
    }

    func loadSegmentEffortDetail(segmentID: Int64, activityID: Int64, tolerance: Double) async -> SegmentEffortDetail? {
        guard isAuthorized else { return nil }
        isLoadingSegmentEffortDetail = true
        defer { isLoadingSegmentEffortDetail = false }

        do {
            let queryItems = [URLQueryItem(name: "tolerance", value: Self.queryNumber(tolerance))]
            guard let url = mobileURL(path: "/api/mobile/segments/\(segmentID)/activities/\(activityID)", queryItems: queryItems) else {
                show("Could not build segment effort URL.")
                return nil
            }
            return try await request(url, authorized: true)
        } catch {
            handleRequestError(error)
            return nil
        }
    }

    func createSegment(activityID: Int64, name: String, description: String, startIndex: Int, endIndex: Int) async -> SegmentDetail? {
        guard isAuthorized else { return nil }
        isMutatingSegment = true
        defer { isMutatingSegment = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/segments") else {
                show("Could not build segment URL.")
                return nil
            }
            let body = SegmentCreateRequest(
                name: name,
                description: description,
                activityID: activityID,
                startIndex: startIndex,
                endIndex: endIndex
            )
            let response: SegmentDetailResponse = try await request(url, method: "POST", body: body, authorized: true)
            await loadSegments()
            show("Segment created.")
            return response.segment
        } catch {
            handleRequestError(error)
            return nil
        }
    }

    func updateSegment(id: Int64, name: String, description: String) async -> SegmentDetail? {
        guard isAuthorized else { return nil }
        isMutatingSegment = true
        defer { isMutatingSegment = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/segments/\(id)") else {
                show("Could not build segment URL.")
                return nil
            }
            let body = SegmentUpdateRequest(name: name, description: description)
            let response: SegmentDetailResponse = try await request(url, method: "PATCH", body: body, authorized: true)
            await loadSegments()
            show("Segment updated.")
            return response.segment
        } catch {
            handleRequestError(error)
            return nil
        }
    }

    func deleteSegment(id: Int64) async -> Bool {
        guard isAuthorized else { return false }
        isMutatingSegment = true
        defer { isMutatingSegment = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/segments/\(id)") else {
                show("Could not build segment URL.")
                return false
            }
            _ = try await requestData(url, method: "DELETE", authorized: true)
            segments.removeAll { $0.id == id }
            show("Segment deleted.")
            return true
        } catch {
            handleRequestError(error)
            return false
        }
    }

    func loadProfile() async {
        guard isAuthorized else { return }
        isLoadingProfile = true
        defer { isLoadingProfile = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/profile") else {
                show("Could not build profile URL.")
                return
            }
            let profile: ProfileSummary = try await request(url, authorized: true)
            self.profile = profile
            if let athlete = profile.athlete {
                self.athlete = athlete
            }
        } catch {
            handleRequestError(error)
        }
    }

    func logout() async {
        guard isAuthorized else {
            clearSession()
            return
        }
        isLoggingOut = true
        defer { isLoggingOut = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/logout") else {
                show("Could not build logout URL.")
                return
            }
            let _: LogoutResponse = try await request(url, method: "POST", authorized: true)
            clearSession()
            show("Logged out.")
        } catch {
            if case AppError.http(let statusCode, _) = error, statusCode == 401 {
                clearSession()
                show("Logged out.")
                return
            }
            handleRequestError(error)
        }
    }

    func loadDiscoveredStatus() async {
        guard isAuthorized else { return }
        isLoadingDiscovered = true
        defer { isLoadingDiscovered = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/discovered/status") else {
                show("Could not build discovered URL.")
                return
            }
            let status: DiscoveredStatus = try await request(url, authorized: true)
            if discoveredMapSnapshot?.sourceBBox != status.bbox {
                discoveredMapSnapshot = nil
            }
            discoveredStatus = status
        } catch {
            handleRequestError(error)
        }
    }

    func rebuildDiscoveredCoverage() async {
        guard isAuthorized else { return }
        isRebuildingDiscovered = true
        defer { isRebuildingDiscovered = false }

        do {
            guard let url = mobileURL(path: "/api/mobile/discovered/rebuild") else {
                show("Could not build rebuild URL.")
                return
            }
            let status: DiscoveredStatus = try await request(url, method: "POST", authorized: true)
            discoveredStatus = status
            discoveredMapSnapshot = nil
            await loadDiscoveredMap()
        } catch {
            handleRequestError(error)
        }
    }

    func loadDiscoveredMap() async {
        guard isAuthorized else { return }
        if discoveredStatus == nil {
            await loadDiscoveredStatus()
        }
        guard let status = discoveredStatus else { return }
        guard let requestBBox = Self.discoveredRequestBBox(from: status.bbox) else {
            discoveredMapSnapshot = nil
            return
        }

        isLoadingDiscoveredMap = true
        defer { isLoadingDiscoveredMap = false }

        do {
            let bbox = Self.bboxQueryValue(requestBBox)
            let queryItems = [URLQueryItem(name: "bbox", value: bbox)]
            guard let fogURL = mobileURL(path: "/api/mobile/discovered/fog", queryItems: queryItems),
                  let coverageURL = mobileURL(path: "/api/mobile/discovered/coverage", queryItems: queryItems) else {
                show("Could not build discovered map URL.")
                return
            }
            let fogGeoJSON = try await requestData(fogURL, authorized: true)
            let coverageGeoJSON = try await requestData(coverageURL, authorized: true)
            discoveredMapSnapshot = DiscoveredMapSnapshot(
                sourceBBox: status.bbox,
                requestBBox: requestBBox,
                fogGeoJSON: fogGeoJSON,
                coverageGeoJSON: coverageGeoJSON
            )
        } catch {
            handleRequestError(error)
        }
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        let windowScenes = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
        let keyWindow = windowScenes
            .flatMap(\.windows)
            .first(where: { $0.isKeyWindow })
        if let keyWindow {
            return keyWindow
        }
        guard let windowScene = windowScenes.first else {
            preconditionFailure("No window scene available for authentication presentation.")
        }
        return ASPresentationAnchor(windowScene: windowScene)
    }

    private var baseURL: URL? {
        let rawValue = baseURLString.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !rawValue.isEmpty,
              var components = URLComponents(string: rawValue),
              let scheme = components.scheme?.lowercased(),
              let host = components.host,
              !host.isEmpty,
              Self.isBackendSchemeAllowed(scheme: scheme, host: host) else {
            return nil
        }
        components.scheme = scheme
        return components.url
    }

    private static func isBackendSchemeAllowed(scheme: String, host: String) -> Bool {
        if scheme == "https" {
            return true
        }

        #if DEBUG
        if scheme == "http" && isLocalOrPrivateHost(host) {
            return true
        }
        #endif

        return false
    }

    private static func isLocalOrPrivateHost(_ host: String) -> Bool {
        let normalized = host
            .trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
            .lowercased()

        if normalized == "localhost" || normalized.hasSuffix(".localhost") || normalized.hasSuffix(".local") {
            return true
        }
        if normalized == "::1" || normalized.hasPrefix("fe80:") || normalized.hasPrefix("fc") || normalized.hasPrefix("fd") {
            return true
        }

        let parts = normalized.split(separator: ".").compactMap { Int($0) }
        guard parts.count == 4, parts.allSatisfy({ (0...255).contains($0) }) else {
            return false
        }
        return parts[0] == 10 ||
            parts[0] == 127 ||
            (parts[0] == 172 && (16...31).contains(parts[1])) ||
            (parts[0] == 169 && parts[1] == 254) ||
            (parts[0] == 192 && parts[1] == 168)
    }

    private func mobileURL(path: String, queryItems: [URLQueryItem] = []) -> URL? {
        guard let baseURL else { return nil }
        var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)
        let basePath = components?.path.trimmingCharacters(in: CharacterSet(charactersIn: "/")) ?? ""
        let requestPath = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        components?.path = "/" + [basePath, requestPath].filter { !$0.isEmpty }.joined(separator: "/")
        if !queryItems.isEmpty {
            components?.queryItems = queryItems
        }
        return components?.url
    }

    private func show(_ text: String) {
        message = text
        showingMessage = true
    }

    private func handleRequestError(_ error: Error) {
        if case AppError.http(let statusCode, _) = error, statusCode == 401 {
            clearSession()
            show("Your local B11K session expired. Connect Strava again.")
            return
        }
        show(error.localizedDescription)
    }

    private func clearSession() {
        setSessionToken("")
        athlete = nil
        activityCount = 0
        activities = []
        segments = []
        profile = nil
        discoveredStatus = nil
        discoveredMapSnapshot = nil
        activityPage = 1
        hasMoreActivities = false
        syncSummary = nil
        rebuildStorage = nil
        isWaitingForBrowserAuth = false
        pendingState = nil
    }

    private func setSessionToken(_ token: String) {
        sessionToken = token
        KeychainSessionStore.save(token)
    }

    private func request<Response: Decodable>(
        _ url: URL,
        method: String = "GET",
        authorized: Bool = false
    ) async throws -> Response {
        try await request(url, method: method, bodyData: nil, authorized: authorized)
    }

    private func request<RequestBody: Encodable, Response: Decodable>(
        _ url: URL,
        method: String = "GET",
        body: RequestBody,
        authorized: Bool = false
    ) async throws -> Response {
        let data = try JSONEncoder().encode(body)
        return try await request(url, method: method, bodyData: data, authorized: authorized)
    }

    private func request<Response: Decodable>(
        _ url: URL,
        method: String,
        bodyData: Data?,
        authorized: Bool
    ) async throws -> Response {
        let data = try await requestData(url, method: method, bodyData: bodyData, authorized: authorized)
        return try JSONDecoder.b11k.decode(Response.self, from: data)
    }

    private func requestData(
        _ url: URL,
        method: String = "GET",
        bodyData: Data? = nil,
        authorized: Bool = false
    ) async throws -> Data {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 600
        if let bodyData {
            request.httpBody = bodyData
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if authorized {
            guard let scheme = url.scheme?.lowercased(),
                  let host = url.host,
                  Self.isBackendSchemeAllowed(scheme: scheme, host: host) else {
                throw AppError.message("Use an HTTPS backend URL before sending an authenticated request.")
            }
            request.setValue("Bearer \(sessionToken)", forHTTPHeaderField: "Authorization")
        }

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw AppError.message("Invalid backend response.")
        }
        guard (200..<300).contains(http.statusCode) else {
            let text = String(data: data, encoding: .utf8) ?? "HTTP \(http.statusCode)"
            throw AppError.http(http.statusCode, text.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        return data
    }

    private static func discoveredRequestBBox(from bbox: [Double]?) -> [Double]? {
        guard let bbox, bbox.count == 4 else { return nil }
        let minLng = bbox[0]
        let minLat = bbox[1]
        let maxLng = bbox[2]
        let maxLat = bbox[3]
        guard minLng.isFinite, minLat.isFinite, maxLng.isFinite, maxLat.isFinite else { return nil }
        guard minLng >= -180, maxLng <= 180, minLat >= -90, maxLat <= 90, minLng <= maxLng, minLat <= maxLat else {
            return nil
        }

        let lngPad = max((maxLng - minLng) * 0.15, 0.01)
        let latPad = max((maxLat - minLat) * 0.15, 0.01)
        return [
            max(-180, minLng - lngPad),
            max(-90, minLat - latPad),
            min(180, maxLng + lngPad),
            min(90, maxLat + latPad)
        ]
    }

    private static func bboxQueryValue(_ bbox: [Double]) -> String {
        bbox.map { value in
            String(format: "%.6f", locale: Locale(identifier: "en_US_POSIX"), arguments: [value])
        }.joined(separator: ",")
    }

    private static func queryNumber(_ value: Double) -> String {
        String(format: "%.1f", locale: Locale(identifier: "en_US_POSIX"), arguments: [value])
    }

    private static let dateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter
    }()
}

struct AuthStartResponse: Decodable {
    let state: String
    let redirectURI: String
    let appAuthURL: String
    let webAuthURL: String

    enum CodingKeys: String, CodingKey {
        case state
        case redirectURI = "redirect_uri"
        case appAuthURL = "app_auth_url"
        case webAuthURL = "web_auth_url"
    }
}

struct AuthExchangeRequest: Encodable {
    let code: String
    let state: String
    let scope: String
}

struct AuthExchangeResponse: Decodable {
    let sessionToken: String
    let athlete: Athlete

    enum CodingKeys: String, CodingKey {
        case sessionToken = "session_token"
        case athlete
    }
}

struct AuthSessionResponse: Decodable {
    let status: String
    let sessionToken: String?
    let athlete: Athlete?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case status
        case sessionToken = "session_token"
        case athlete
        case error
    }
}

struct LogoutResponse: Decodable {
    let loggedOut: Bool

    enum CodingKeys: String, CodingKey {
        case loggedOut = "logged_out"
    }
}

struct MeResponse: Decodable {
    let athlete: Athlete
}

struct ActivitiesResponse: Decodable {
    let count: Int
    let page: Int
    let perPage: Int
    let hasMore: Bool
    let activities: [Activity]

    enum CodingKeys: String, CodingKey {
        case count
        case page
        case perPage = "per_page"
        case hasMore = "has_more"
        case activities
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        activities = try container.decodeIfPresent([Activity].self, forKey: .activities) ?? []
        count = try container.decodeIfPresent(Int.self, forKey: .count) ?? activities.count
        page = try container.decodeIfPresent(Int.self, forKey: .page) ?? 1
        perPage = try container.decodeIfPresent(Int.self, forKey: .perPage) ?? max(activities.count, 1)
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? (activities.count < count)
    }
}

struct ActivityRouteResponse: Decodable {
    let activityID: Int64
    let count: Int
    let source: String
    let points: [RoutePoint]

    enum CodingKeys: String, CodingKey {
        case activityID = "activity_id"
        case count
        case source
        case points
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        points = try container.decodeIfPresent([RoutePoint].self, forKey: .points) ?? []
        activityID = try container.decodeIfPresent(Int64.self, forKey: .activityID) ?? 0
        count = try container.decodeIfPresent(Int.self, forKey: .count) ?? points.count
        source = try container.decodeIfPresent(String.self, forKey: .source) ?? "unknown"
    }
}

struct ProfileSummary: Decodable {
    let athlete: Athlete?
    let hrZones: [ProfileHRZone]
    let hrZonesError: String
    let totalBikeKM: Double
    let totalActivities: Int
    let bikeStats: [ProfileBikeStat]
    let bestMonth: ProfilePeriodStat
    let bestYear: ProfilePeriodStat
    let hasRecordedRides: Bool
    let hasRecordedMonths: Bool

    enum CodingKeys: String, CodingKey {
        case athlete
        case hrZones = "hr_zones"
        case hrZonesError = "hr_zones_error"
        case totalBikeKM = "total_bike_km"
        case totalActivities = "total_activities"
        case bikeStats = "bike_stats"
        case bestMonth = "best_month"
        case bestYear = "best_year"
        case hasRecordedRides = "has_recorded_rides"
        case hasRecordedMonths = "has_recorded_months"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        athlete = try container.decodeIfPresent(Athlete.self, forKey: .athlete)
        hrZones = try container.decodeIfPresent([ProfileHRZone].self, forKey: .hrZones) ?? []
        hrZonesError = try container.decodeIfPresent(String.self, forKey: .hrZonesError) ?? ""
        totalBikeKM = try container.decodeIfPresent(Double.self, forKey: .totalBikeKM) ?? 0
        totalActivities = try container.decodeIfPresent(Int.self, forKey: .totalActivities) ?? 0
        bikeStats = try container.decodeIfPresent([ProfileBikeStat].self, forKey: .bikeStats) ?? []
        bestMonth = try container.decodeIfPresent(ProfilePeriodStat.self, forKey: .bestMonth) ?? ProfilePeriodStat.empty
        bestYear = try container.decodeIfPresent(ProfilePeriodStat.self, forKey: .bestYear) ?? ProfilePeriodStat.empty
        hasRecordedRides = try container.decodeIfPresent(Bool.self, forKey: .hasRecordedRides) ?? !bikeStats.isEmpty
        hasRecordedMonths = try container.decodeIfPresent(Bool.self, forKey: .hasRecordedMonths) ?? !bestMonth.label.isEmpty || !bestYear.label.isEmpty
    }
}

struct ProfileBikeStat: Decodable, Identifiable {
    let gearID: String
    let label: String
    let distanceKM: Double
    let activities: Int

    var id: String { gearID }

    enum CodingKeys: String, CodingKey {
        case gearID = "gear_id"
        case label
        case distanceKM = "distance_km"
        case activities
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        gearID = try container.decodeIfPresent(String.self, forKey: .gearID) ?? UUID().uuidString
        label = try container.decodeIfPresent(String.self, forKey: .label) ?? "Unknown Bike"
        distanceKM = try container.decodeIfPresent(Double.self, forKey: .distanceKM) ?? 0
        activities = try container.decodeIfPresent(Int.self, forKey: .activities) ?? 0
    }
}

struct ProfilePeriodStat: Decodable {
    static let empty = ProfilePeriodStat(label: "", activities: 0)

    let label: String
    let activities: Int

    enum CodingKeys: String, CodingKey {
        case label
        case activities
    }

    init(label: String, activities: Int) {
        self.label = label
        self.activities = activities
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        label = try container.decodeIfPresent(String.self, forKey: .label) ?? ""
        activities = try container.decodeIfPresent(Int.self, forKey: .activities) ?? 0
    }
}

struct ProfileHRZone: Decodable, Identifiable {
    let label: String
    let range: String

    var id: String { "\(label)-\(range)" }

    enum CodingKeys: String, CodingKey {
        case label
        case range
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        label = try container.decodeIfPresent(String.self, forKey: .label) ?? "Zone"
        range = try container.decodeIfPresent(String.self, forKey: .range) ?? "not set"
    }
}

struct SegmentsResponse: Decodable {
    let count: Int
    let segments: [SegmentSummary]

    enum CodingKeys: String, CodingKey {
        case count
        case segments
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        segments = try container.decodeIfPresent([SegmentSummary].self, forKey: .segments) ?? []
        count = try container.decodeIfPresent(Int.self, forKey: .count) ?? segments.count
    }
}

struct SegmentCreateRequest: Encodable {
    let name: String
    let description: String
    let activityID: Int64
    let startIndex: Int
    let endIndex: Int

    enum CodingKeys: String, CodingKey {
        case name
        case description
        case activityID = "activity_id"
        case startIndex = "start_index"
        case endIndex = "end_index"
    }
}

struct SegmentUpdateRequest: Encodable {
    let name: String
    let description: String
}

struct SegmentDetailResponse: Decodable {
    let segment: SegmentDetail
}

enum SegmentEffortSort: String, CaseIterable, Identifiable {
    case totalTime = "total_time"
    case date
    case distance
    case avgHR = "avg_hr"
    case avgSpeed = "avg_speed"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .totalTime:
            return "Best Time"
        case .date:
            return "Latest"
        case .distance:
            return "Best Match"
        case .avgHR:
            return "Avg HR"
        case .avgSpeed:
            return "Avg Speed"
        }
    }
}

struct SegmentEffortsResponse: Decodable {
    let segmentID: Int64
    let count: Int
    let tolerance: Double
    let sort: String
    let activities: [SegmentEffort]

    enum CodingKeys: String, CodingKey {
        case segmentID = "segment_id"
        case count
        case tolerance
        case sort
        case activities
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        activities = try container.decodeIfPresent([SegmentEffort].self, forKey: .activities) ?? []
        segmentID = try container.decodeIfPresent(Int64.self, forKey: .segmentID) ?? 0
        count = try container.decodeIfPresent(Int.self, forKey: .count) ?? activities.count
        tolerance = try container.decodeIfPresent(Double.self, forKey: .tolerance) ?? 15
        sort = try container.decodeIfPresent(String.self, forKey: .sort) ?? SegmentEffortSort.totalTime.rawValue
    }
}

struct SegmentEffortDetail: Decodable {
    let segmentID: Int64
    let activityID: Int64
    let tolerance: Double
    let startIndex: Int
    let endIndex: Int
    let activity: Activity
    let metrics: SegmentEffortMetrics
    let points: [RoutePoint]

    enum CodingKeys: String, CodingKey {
        case segmentID = "segment_id"
        case activityID = "activity_id"
        case tolerance
        case startIndex = "start_index"
        case endIndex = "end_index"
        case activity
        case metrics
        case points
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        segmentID = try container.decodeIfPresent(Int64.self, forKey: .segmentID) ?? 0
        activityID = try container.decodeIfPresent(Int64.self, forKey: .activityID) ?? 0
        tolerance = try container.decodeIfPresent(Double.self, forKey: .tolerance) ?? 15
        startIndex = try container.decodeIfPresent(Int.self, forKey: .startIndex) ?? 0
        endIndex = try container.decodeIfPresent(Int.self, forKey: .endIndex) ?? 0
        activity = try container.decode(Activity.self, forKey: .activity)
        metrics = try container.decodeIfPresent(SegmentEffortMetrics.self, forKey: .metrics) ?? SegmentEffortMetrics.empty
        points = try container.decodeIfPresent([RoutePoint].self, forKey: .points) ?? []
    }
}

struct SegmentEffortMetrics: Decodable {
    static let empty = SegmentEffortMetrics(avgHR: 0, avgSpeed: 0, distance: 0, elevationGain: 0, elapsedSeconds: 0)

    let avgHR: Double
    let avgSpeed: Double
    let distance: Double
    let elevationGain: Double
    let elapsedSeconds: Double

    enum CodingKeys: String, CodingKey {
        case avgHR = "avg_hr"
        case avgSpeed = "avg_speed"
        case distance
        case elevationGain = "elevation_gain"
        case elapsedSeconds = "elapsed_seconds"
    }

    init(avgHR: Double, avgSpeed: Double, distance: Double, elevationGain: Double, elapsedSeconds: Double) {
        self.avgHR = avgHR
        self.avgSpeed = avgSpeed
        self.distance = distance
        self.elevationGain = elevationGain
        self.elapsedSeconds = elapsedSeconds
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        avgHR = try container.decodeIfPresent(Double.self, forKey: .avgHR) ?? 0
        avgSpeed = try container.decodeIfPresent(Double.self, forKey: .avgSpeed) ?? 0
        distance = try container.decodeIfPresent(Double.self, forKey: .distance) ?? 0
        elevationGain = try container.decodeIfPresent(Double.self, forKey: .elevationGain) ?? 0
        elapsedSeconds = try container.decodeIfPresent(Double.self, forKey: .elapsedSeconds) ?? 0
    }
}

struct SegmentEffort: Decodable, Identifiable {
    let activity: Activity
    let minDistanceM: Double
    let overlapLengthM: Double
    let overlapPercentage: Double
    let segmentAvgHR: Double?
    let segmentAvgSpeed: Double?
    let segmentDistance: Double?
    let segmentElevation: Double?
    let segmentElapsedSecs: Double?

    var id: Int64 { activity.id }

    enum CodingKeys: String, CodingKey {
        case activity
        case minDistanceM = "min_distance_m"
        case overlapLengthM = "overlap_length_m"
        case overlapPercentage = "overlap_percentage"
        case segmentAvgHR = "segment_avg_hr"
        case segmentAvgSpeed = "segment_avg_speed"
        case segmentDistance = "segment_distance"
        case segmentElevation = "segment_elevation_gain"
        case segmentElapsedSecs = "segment_elapsed_seconds"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        activity = try container.decode(Activity.self, forKey: .activity)
        minDistanceM = try container.decodeIfPresent(Double.self, forKey: .minDistanceM) ?? 0
        overlapLengthM = try container.decodeIfPresent(Double.self, forKey: .overlapLengthM) ?? 0
        overlapPercentage = try container.decodeIfPresent(Double.self, forKey: .overlapPercentage) ?? 0
        segmentAvgHR = try container.decodeIfPresent(Double.self, forKey: .segmentAvgHR)
        segmentAvgSpeed = try container.decodeIfPresent(Double.self, forKey: .segmentAvgSpeed)
        segmentDistance = try container.decodeIfPresent(Double.self, forKey: .segmentDistance)
        segmentElevation = try container.decodeIfPresent(Double.self, forKey: .segmentElevation)
        segmentElapsedSecs = try container.decodeIfPresent(Double.self, forKey: .segmentElapsedSecs)
    }
}

struct SegmentSummary: Decodable, Identifiable {
    let id: Int64
    let name: String
    let description: String?
    let createdAt: String
    let distanceLabel: String
    let netRiseLabel: String
    let ascentLabel: String
    let slopeLabel: String
    let direction: String
    let directionKey: String
    let attempts: Int
    let minTimeLabel: String
    let maxTimeLabel: String
    let minHRLabel: String
    let maxHRLabel: String

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case description
        case createdAt = "created_at"
        case distanceLabel = "distance_label"
        case netRiseLabel = "net_rise_label"
        case ascentLabel = "ascent_label"
        case slopeLabel = "slope_label"
        case direction
        case directionKey = "direction_key"
        case attempts
        case minTimeLabel = "min_time_label"
        case maxTimeLabel = "max_time_label"
        case minHRLabel = "min_hr_label"
        case maxHRLabel = "max_hr_label"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(Int64.self, forKey: .id) ?? 0
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? "Untitled Segment"
        description = try container.decodeIfPresent(String.self, forKey: .description)
        createdAt = try container.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
        distanceLabel = try container.decodeIfPresent(String.self, forKey: .distanceLabel) ?? "n/a"
        netRiseLabel = try container.decodeIfPresent(String.self, forKey: .netRiseLabel) ?? "n/a"
        ascentLabel = try container.decodeIfPresent(String.self, forKey: .ascentLabel) ?? "n/a"
        slopeLabel = try container.decodeIfPresent(String.self, forKey: .slopeLabel) ?? "n/a"
        direction = try container.decodeIfPresent(String.self, forKey: .direction) ?? "Unknown"
        directionKey = try container.decodeIfPresent(String.self, forKey: .directionKey) ?? "unknown"
        attempts = try container.decodeIfPresent(Int.self, forKey: .attempts) ?? 0
        minTimeLabel = try container.decodeIfPresent(String.self, forKey: .minTimeLabel) ?? "n/a"
        maxTimeLabel = try container.decodeIfPresent(String.self, forKey: .maxTimeLabel) ?? "n/a"
        minHRLabel = try container.decodeIfPresent(String.self, forKey: .minHRLabel) ?? "n/a"
        maxHRLabel = try container.decodeIfPresent(String.self, forKey: .maxHRLabel) ?? "n/a"
    }
}

struct SegmentDetail: Decodable, Identifiable {
    let id: Int64
    let name: String
    let description: String?
    let createdAt: String
    let updatedAt: String
    let distanceMeters: Double?
    let elevationGainM: Double?
    let elevationLossM: Double?
    let netElevationM: Double?
    let slopePercent: Double?
    let direction: String
    let directionKey: String
    let geometry: SegmentGeometry?

    var routePoints: [RoutePoint] {
        geometry?.points.enumerated().map { index, point in
            RoutePoint(index: index, lat: point.lat, lng: point.lng)
        } ?? []
    }

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case description
        case createdAt = "created_at"
        case updatedAt = "updated_at"
        case distanceMeters = "distance_meters"
        case elevationGainM = "elevation_gain_m"
        case elevationLossM = "elevation_loss_m"
        case netElevationM = "net_elevation_m"
        case slopePercent = "slope_percent"
        case direction
        case directionKey = "direction_key"
        case geometry
    }
}

struct SegmentGeometry: Decodable {
    let type: String
    let coordinates: [[Double]]
    let points: [SegmentPoint]
}

struct SegmentPoint: Decodable {
    let lat: Double
    let lng: Double
}

struct DiscoveredStatus: Decodable {
    let athleteID: Int64
    let enabled: Bool
    let stale: Bool
    let buildableActivities: Int
    let cachedActivities: Int
    let radiusMeters: Double
    let sampleDistanceMeters: Double
    let rebuiltAt: Date?
    let bbox: [Double]?
    let message: String?

    var statusLabel: String {
        if !enabled {
            return "Disabled"
        }
        return stale ? "Stale" : "Current"
    }

    var mapCacheKey: String {
        let bboxKey = bbox?.map { Formatters.coordinate($0) }.joined(separator: ",") ?? "none"
        let rebuiltKey = rebuiltAt.map { String($0.timeIntervalSince1970) } ?? "never"
        return "\(bboxKey)-\(rebuiltKey)-\(stale)-\(cachedActivities)"
    }

    enum CodingKeys: String, CodingKey {
        case athleteID = "athlete_id"
        case enabled
        case stale
        case buildableActivities = "buildable_activities"
        case cachedActivities = "cached_activities"
        case radiusMeters = "radius_meters"
        case sampleDistanceMeters = "sample_distance_meters"
        case rebuiltAt = "rebuilt_at"
        case bbox
        case message
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        athleteID = try container.decodeIfPresent(Int64.self, forKey: .athleteID) ?? 0
        enabled = try container.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        stale = try container.decodeIfPresent(Bool.self, forKey: .stale) ?? true
        buildableActivities = try container.decodeIfPresent(Int.self, forKey: .buildableActivities) ?? 0
        cachedActivities = try container.decodeIfPresent(Int.self, forKey: .cachedActivities) ?? 0
        radiusMeters = try container.decodeIfPresent(Double.self, forKey: .radiusMeters) ?? 0
        sampleDistanceMeters = try container.decodeIfPresent(Double.self, forKey: .sampleDistanceMeters) ?? 0
        if let rebuiltAtString = try container.decodeIfPresent(String.self, forKey: .rebuiltAt) {
            rebuiltAt = Formatters.isoDate(from: rebuiltAtString)
        } else {
            rebuiltAt = nil
        }
        bbox = try container.decodeIfPresent([Double].self, forKey: .bbox)
        message = try container.decodeIfPresent(String.self, forKey: .message)
    }
}

struct DiscoveredMapSnapshot {
    let sourceBBox: [Double]?
    let requestBBox: [Double]
    let fogGeoJSON: Data
    let coverageGeoJSON: Data

    var renderKey: String {
        let bboxKey = requestBBox.map { Formatters.coordinate($0) }.joined(separator: ",")
        return "\(bboxKey)-\(fogGeoJSON.count)-\(coverageGeoJSON.count)"
    }
}

struct RouteSnapshot {
    let source: String
    let count: Int
    let points: [RoutePoint]

    var sourceLabel: String {
        switch source {
        case "point_samples":
            return "point_samples"
        case "activity_geometries":
            return "activity_geometries"
        case "none":
            return "none"
        default:
            return source.isEmpty ? "unknown" : source
        }
    }

    var chartMetrics: [RoutePaintMetric] {
        RoutePaintMetric.chartMetrics.filter { metric in
            points.contains { metric.value(from: $0) != nil }
        }
    }

    var hasChartData: Bool {
        !chartMetrics.isEmpty
    }

    func hasData(for metric: RoutePaintMetric) -> Bool {
        metric == .none || sampleCount(for: metric) > 0
    }

    func sampleCount(for metric: RoutePaintMetric) -> Int {
        points.reduce(0) { count, point in
            metric.value(from: point) == nil ? count : count + 1
        }
    }

    var maxSpeed: Double? {
        points.compactMap(\.speed).max()
    }

    var maxCadence: Double? {
        points.compactMap { $0.cadence.map(Double.init) }.max()
    }

    var maxGrade: Double? {
        points.compactMap(\.grade).max()
    }

    var maxElevation: Double? {
        points.compactMap(\.altitude).max()
    }
}

struct RoutePoint: Decodable, Identifiable {
    let index: Int
    let lat: Double
    let lng: Double
    let altitude: Double?
    let heartrate: Int?
    let speed: Double?
    let watts: Int?
    let cadence: Int?
    let grade: Double?
    let moving: Bool?
    let cumulativeDistance: Double?

    var id: Int { index }

    var chartX: Double {
        if let cumulativeDistance, cumulativeDistance > 0 {
            return cumulativeDistance / 1000
        }
        return Double(index)
    }

    enum CodingKeys: String, CodingKey {
        case index
        case lat
        case lng
        case altitude
        case heartrate
        case speed
        case watts
        case cadence
        case grade
        case moving
        case cumulativeDistance = "cumulative_distance"
    }

    init(
        index: Int,
        lat: Double,
        lng: Double,
        altitude: Double? = nil,
        heartrate: Int? = nil,
        speed: Double? = nil,
        watts: Int? = nil,
        cadence: Int? = nil,
        grade: Double? = nil,
        moving: Bool? = nil,
        cumulativeDistance: Double? = nil
    ) {
        self.index = index
        self.lat = lat
        self.lng = lng
        self.altitude = altitude
        self.heartrate = heartrate
        self.speed = speed
        self.watts = watts
        self.cadence = cadence
        self.grade = grade
        self.moving = moving
        self.cumulativeDistance = cumulativeDistance
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        index = try container.decodeIfPresent(Int.self, forKey: .index) ?? 0
        lat = try container.decodeIfPresent(Double.self, forKey: .lat) ?? 0
        lng = try container.decodeIfPresent(Double.self, forKey: .lng) ?? 0
        altitude = try container.decodeIfPresent(Double.self, forKey: .altitude)
        heartrate = try container.decodeIfPresent(Int.self, forKey: .heartrate)
        speed = try container.decodeIfPresent(Double.self, forKey: .speed)
        watts = try container.decodeIfPresent(Int.self, forKey: .watts)
        cadence = try container.decodeIfPresent(Int.self, forKey: .cadence)
        grade = try container.decodeIfPresent(Double.self, forKey: .grade)
        moving = try container.decodeIfPresent(Bool.self, forKey: .moving)
        cumulativeDistance = try container.decodeIfPresent(Double.self, forKey: .cumulativeDistance)
    }
}

enum RoutePaintMetric: String, CaseIterable, Identifiable {
    case none
    case heartrate
    case speed
    case grade
    case altitude
    case cadence
    case watts

    var id: String { rawValue }

    static let paintMetrics: [RoutePaintMetric] = [.none, .heartrate, .speed, .grade, .altitude, .cadence, .watts]
    static let chartMetrics: [RoutePaintMetric] = [.heartrate, .speed, .grade, .altitude, .cadence, .watts]

    var title: String {
        switch self {
        case .none:
            return "Plain"
        case .heartrate:
            return "Heart Rate"
        case .speed:
            return "Speed"
        case .grade:
            return "Slope"
        case .altitude:
            return "Elevation"
        case .cadence:
            return "Cadence"
        case .watts:
            return "Power"
        }
    }

    func value(from point: RoutePoint) -> Double? {
        switch self {
        case .none:
            return nil
        case .heartrate:
            return point.heartrate.map(Double.init)
        case .speed:
            return point.speed
        case .grade:
            return point.grade
        case .altitude:
            return point.altitude
        case .cadence:
            return point.cadence.map(Double.init)
        case .watts:
            return point.watts.map(Double.init)
        }
    }

    func format(_ value: Double) -> String {
        switch self {
        case .none:
            return ""
        case .heartrate:
            return "\(Formatters.wholeNumber(value)) bpm"
        case .speed:
            return Formatters.speed(value)
        case .grade:
            return "\(Formatters.number(value))%"
        case .altitude:
            return Formatters.elevation(value)
        case .cadence:
            return "\(Formatters.wholeNumber(value)) rpm"
        case .watts:
            return "\(Formatters.wholeNumber(value)) W"
        }
    }

    var chartColor: Color {
        switch self {
        case .none:
            return .orange
        case .heartrate:
            return .red
        case .speed:
            return .blue
        case .grade:
            return .yellow
        case .altitude:
            return .green
        case .cadence:
            return .purple
        case .watts:
            return .orange
        }
    }

    func color(for value: Double, min minValue: Double, max maxValue: Double) -> UIColor {
        guard maxValue > minValue else { return .systemOrange }
        let ratio = Swift.min(1, Swift.max(0, (value - minValue) / (maxValue - minValue)))
        switch ratio {
        case ..<0.25:
            return .systemTeal
        case ..<0.5:
            return .systemGreen
        case ..<0.75:
            return .systemYellow
        default:
            return .systemRed
        }
    }
}

struct Activity: Decodable, Identifiable {
    let id: Int64
    let name: String
    let type: String
    let sportType: String
    let startDate: Date?
    let distance: Double
    let movingTime: Double
    let elapsedTime: Double
    let totalElevationGain: Double
    let locationCity: String?
    let locationState: String?
    let locationCountry: String?
    let gearID: String
    let gearName: String?
    let averageSpeed: Double
    let maxSpeed: Double
    let averageCadence: Double
    let averageWatts: Double
    let kilojoules: Double
    let averageHeartrate: Double
    let maxHeartrate: Double
    let maxWatts: Double
    let sufferScore: Double

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(Int64.self, forKey: .id) ?? 0
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? "Untitled Activity"
        type = try container.decodeIfPresent(String.self, forKey: .type) ?? ""
        sportType = try container.decodeIfPresent(String.self, forKey: .sportType) ?? ""
        let startDateString = try container.decodeIfPresent(String.self, forKey: .startDate) ?? ""
        startDate = Formatters.isoDate(from: startDateString)
        distance = try container.decodeIfPresent(Double.self, forKey: .distance) ?? 0
        movingTime = try container.decodeIfPresent(Double.self, forKey: .movingTime) ?? 0
        elapsedTime = try container.decodeIfPresent(Double.self, forKey: .elapsedTime) ?? 0
        totalElevationGain = try container.decodeIfPresent(Double.self, forKey: .totalElevationGain) ?? 0
        locationCity = try container.decodeIfPresent(String.self, forKey: .locationCity)
        locationState = try container.decodeIfPresent(String.self, forKey: .locationState)
        locationCountry = try container.decodeIfPresent(String.self, forKey: .locationCountry)
        gearID = try container.decodeIfPresent(String.self, forKey: .gearID) ?? ""
        gearName = try container.decodeIfPresent(String.self, forKey: .gearName)
        averageSpeed = try container.decodeIfPresent(Double.self, forKey: .averageSpeed) ?? 0
        maxSpeed = try container.decodeIfPresent(Double.self, forKey: .maxSpeed) ?? 0
        averageCadence = try container.decodeIfPresent(Double.self, forKey: .averageCadence) ?? 0
        averageWatts = try container.decodeIfPresent(Double.self, forKey: .averageWatts) ?? 0
        kilojoules = try container.decodeIfPresent(Double.self, forKey: .kilojoules) ?? 0
        averageHeartrate = try container.decodeIfPresent(Double.self, forKey: .averageHeartrate) ?? 0
        maxHeartrate = try container.decodeIfPresent(Double.self, forKey: .maxHeartrate) ?? 0
        maxWatts = try container.decodeIfPresent(Double.self, forKey: .maxWatts) ?? 0
        sufferScore = try container.decodeIfPresent(Double.self, forKey: .sufferScore) ?? 0
    }

    var displayType: String {
        sportType.isEmpty ? type : sportType
    }

    var locationText: String? {
        let parts = [locationCity, locationState, locationCountry]
            .compactMap { $0?.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: ", ")
    }

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case type
        case sportType = "sport_type"
        case startDate = "start_date"
        case distance
        case movingTime = "moving_time"
        case elapsedTime = "elapsed_time"
        case totalElevationGain = "total_elevation_gain"
        case locationCity = "location_city"
        case locationState = "location_state"
        case locationCountry = "location_country"
        case gearID = "gear_id"
        case gearName = "gear_name"
        case averageSpeed = "average_speed"
        case maxSpeed = "max_speed"
        case averageCadence = "average_cadence"
        case averageWatts = "average_watts"
        case kilojoules
        case averageHeartrate = "average_heartrate"
        case maxHeartrate = "max_heartrate"
        case maxWatts = "max_watts"
        case sufferScore = "suffer_score"
    }
}

struct ActivityStats {
    let count: Int
    let distance: Double
    let elevation: Double
    let movingTime: Double
}

struct SyncResponse: Decodable {
    let summary: SyncSummary
    let logs: [String]
}

struct SyncSummary: Decodable {
    let total: Int
    let existing: Int
    let new: Int
    let success: Int
    let failed: Int
}

struct RebuildSyncRequest: Encodable {
    let confirmation: String
}

struct RebuildSyncResponse: Decodable {
    let summary: SyncSummary
    let logs: [String]
    let storage: RebuildStorage
}

struct RebuildStorage: Decodable {
    let activities: Int
    let activityGeometries: Int
    let pointSamples: Int
    let activitiesWithPointSamples: Int
    let activitiesWithGeometry: Int

    enum CodingKeys: String, CodingKey {
        case activities
        case activityGeometries = "activity_geometries"
        case pointSamples = "point_samples"
        case activitiesWithPointSamples = "activities_with_point_samples"
        case activitiesWithGeometry = "activities_with_geometry"
    }
}

struct Athlete: Decodable {
    let id: Int64
    let firstname: String
    let lastname: String
    let profile: String

    enum CodingKeys: String, CodingKey {
        case id
        case firstname
        case lastname
        case profile
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(Int64.self, forKey: .id) ?? 0
        firstname = try container.decodeIfPresent(String.self, forKey: .firstname) ?? ""
        lastname = try container.decodeIfPresent(String.self, forKey: .lastname) ?? ""
        profile = try container.decodeIfPresent(String.self, forKey: .profile) ?? ""
    }
}

enum AppError: LocalizedError {
    case message(String)
    case http(Int, String)

    var errorDescription: String? {
        switch self {
        case .message(let message):
            return message
        case .http(let statusCode, let message):
            return message.isEmpty ? "HTTP \(statusCode)" : message
        }
    }
}

enum Formatters {
    static func isoDate(from value: String) -> Date? {
        guard !value.isEmpty else { return nil }
        return ISO8601DateFormatter.b11k.date(from: value) ?? ISO8601DateFormatter.b11kFractional.date(from: value)
    }

    static func date(_ value: Date?) -> String {
        guard let value else { return "Unknown date" }
        return shortDate.string(from: value)
    }

    static func longDate(_ value: Date?) -> String {
        guard let value else { return "Unknown date" }
        return longDateFormatter.string(from: value)
    }

    static func distance(_ meters: Double) -> String {
        let km = meters / 1000
        return "\(number(km)) km"
    }

    static func elevation(_ meters: Double) -> String {
        "\(wholeNumber(meters)) m"
    }

    static func signedElevation(_ meters: Double) -> String {
        let prefix = meters > 0 ? "+" : ""
        return "\(prefix)\(wholeNumber(meters)) m"
    }

    static func speed(_ metersPerSecond: Double) -> String {
        let kmh = metersPerSecond * 3.6
        return "\(number(kmh)) km/h"
    }

    static func duration(_ seconds: Double) -> String {
        let totalMinutes = max(0, Int(seconds.rounded())) / 60
        let hours = totalMinutes / 60
        let minutes = totalMinutes % 60
        if hours > 0 {
            return "\(hours)h \(minutes)m"
        }
        return "\(minutes)m"
    }

    static func number(_ value: Double) -> String {
        decimal.string(from: NSNumber(value: value)) ?? "\(value)"
    }

    static func wholeNumber(_ value: Double) -> String {
        integer.string(from: NSNumber(value: value)) ?? "\(Int(value))"
    }

    static func coordinate(_ value: Double) -> String {
        coordinateFormatter.string(from: NSNumber(value: value)) ?? "\(value)"
    }

    private static let shortDate: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter
    }()

    private static let longDateFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .full
        formatter.timeStyle = .short
        return formatter
    }()

    private static let decimal: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.maximumFractionDigits = 1
        formatter.minimumFractionDigits = 0
        return formatter
    }()

    private static let integer: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.maximumFractionDigits = 0
        return formatter
    }()

    private static let coordinateFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.maximumFractionDigits = 5
        formatter.minimumFractionDigits = 0
        return formatter
    }()
}

extension ISO8601DateFormatter {
    static let b11k: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    static let b11kFractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
}

extension JSONDecoder {
    static let b11k: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()
}
