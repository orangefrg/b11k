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
                SyncView(viewModel: viewModel)
                    .navigationTitle("B11K")
                    .toolbar {
                        if viewModel.isBusy {
                            ProgressView()
                        }
                    }
            }
            .tabItem {
                Label("Sync", systemImage: "arrow.triangle.2.circlepath")
            }

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

struct SyncView: View {
    @ObservedObject var viewModel: AppViewModel
    @FocusState private var focusedField: SyncField?

    private enum SyncField: Hashable {
        case backendURL
        case rebuildConfirmation
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

            Section("Live Test") {
                TextField("REBUILD", text: $viewModel.rebuildConfirmation)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .focused($focusedField, equals: .rebuildConfirmation)
                    .submitLabel(.done)
                    .onSubmit {
                        focusedField = nil
                    }

                Button(viewModel.isRebuildingDatabase ? "Rebuilding..." : "Rebuild DB and Sync") {
                    focusedField = nil
                    Task { await viewModel.rebuildDatabaseAndSync() }
                }
                .disabled(!viewModel.canRebuildDatabase)

                if let storage = viewModel.rebuildStorage {
                    LabeledContent("Activities", value: "\(storage.activities)")
                    LabeledContent("Route geometries", value: "\(storage.activityGeometries)")
                    LabeledContent("Sample rows", value: "\(storage.pointSamples)")
                    LabeledContent("Activities with samples", value: "\(storage.activitiesWithPointSamples)")
                    LabeledContent("Activities with geometry", value: "\(storage.activitiesWithGeometry)")
                }
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

struct ActivitiesView: View {
    @ObservedObject var viewModel: AppViewModel
    @FocusState private var isSearchFocused: Bool

    var body: some View {
        List {
            if !viewModel.isAuthorized {
                ContentUnavailableView(
                    "Connect Strava",
                    systemImage: "figure.outdoor.cycle",
                    description: Text("Use the Sync tab to connect to your local B11K backend.")
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
    @AppStorage("b11k.sessionToken") private var sessionToken = ""

    @Published var athlete: Athlete?
    @Published var activityCount = 0
    @Published var activities: [Activity] = []
    @Published var activitySearch = ""
    @Published var activityPage = 1
    @Published var hasMoreActivities = false
    @Published var syncSummary: SyncSummary?
    @Published var logLines: [String] = []
    @Published var message = ""
    @Published var showingMessage = false
    @Published var isAuthenticating = false
    @Published var isSyncing = false
    @Published var isRebuildingDatabase = false
    @Published var isLoadingActivities = false
    @Published var isWaitingForBrowserAuth = false
    @Published var rebuildConfirmation = ""
    @Published var rebuildStorage: RebuildStorage?

    @Published var startDate: Date = Calendar.current.date(byAdding: .month, value: -1, to: Date()) ?? Date()
    @Published var endDate: Date = Date()

    private var pendingState: String?
    private var authSession: ASWebAuthenticationSession?

    var isAuthorized: Bool {
        !sessionToken.isEmpty
    }

    var isBusy: Bool {
        isAuthenticating || isSyncing || isRebuildingDatabase || isLoadingActivities
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

            if start.redirectURI.hasPrefix("http://") || start.redirectURI.hasPrefix("https://") {
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
            sessionToken = response.sessionToken
            athlete = response.athlete
            pendingState = nil
            isWaitingForBrowserAuth = false
            logLines.insert("Connected to Strava.", at: 0)
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
                sessionToken = token
                self.athlete = athlete
                isWaitingForBrowserAuth = false
                self.pendingState = nil
                logLines.insert("Connected to Strava.", at: 0)
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
            await loadActivities(reset: true)
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
            await loadActivities(reset: true)
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

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        let keyWindow = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: { $0.isKeyWindow })
        if let keyWindow {
            return keyWindow
        }
        return ASPresentationAnchor(frame: .zero)
    }

    private var baseURL: URL? {
        URL(string: baseURLString.trimmingCharacters(in: .whitespacesAndNewlines))
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
        sessionToken = ""
        athlete = nil
        activityCount = 0
        activities = []
        activityPage = 1
        hasMoreActivities = false
        syncSummary = nil
        rebuildStorage = nil
        isWaitingForBrowserAuth = false
        pendingState = nil
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
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 600
        if let bodyData {
            request.httpBody = bodyData
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if authorized {
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
        return try JSONDecoder.b11k.decode(Response.self, from: data)
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

    enum CodingKeys: String, CodingKey {
        case id
        case firstname
        case lastname
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(Int64.self, forKey: .id) ?? 0
        firstname = try container.decodeIfPresent(String.self, forKey: .firstname) ?? ""
        lastname = try container.decodeIfPresent(String.self, forKey: .lastname) ?? ""
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
