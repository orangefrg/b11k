import SwiftUI

struct SegmentsView: View {
    @ObservedObject var viewModel: AppViewModel
    @FocusState private var isSearchFocused: Bool

    var body: some View {
        List {
            if !viewModel.isAuthorized {
                ContentUnavailableView(
                    "Connect Strava",
                    systemImage: "point.topleft.down.curvedto.point.bottomright.up",
                    description: Text("Use Settings to connect to B11K.")
                )
            } else {
                Section("Search") {
                    TextField("Segment name", text: $viewModel.segmentSearch)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($isSearchFocused)
                        .submitLabel(.search)
                        .onSubmit {
                            isSearchFocused = false
                            Task { await viewModel.loadSegments() }
                        }

                    Button("Search Segments") {
                        isSearchFocused = false
                        Task { await viewModel.loadSegments() }
                    }
                    .disabled(viewModel.isBusy)
                }

                if viewModel.segments.isEmpty && !viewModel.isLoadingSegments {
                    ContentUnavailableView(
                        "No Segments",
                        systemImage: "point.topleft.down.curvedto.point.bottomright.up",
                        description: Text("Create segments from activity routes.")
                    )
                } else {
                    Section("Segments") {
                        ForEach(viewModel.segments) { segment in
                            NavigationLink {
                                SegmentDetailView(viewModel: viewModel, segmentID: segment.id, title: segment.name)
                            } label: {
                                SegmentRow(segment: segment)
                            }
                        }
                    }
                }
            }
        }
        .overlay {
            if viewModel.isLoadingSegments {
                ProgressView("Loading segments...")
            }
        }
        .task {
            if viewModel.isAuthorized && viewModel.segments.isEmpty {
                await viewModel.loadSegments()
            }
        }
        .refreshable {
            await viewModel.loadSegments()
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

struct SegmentRow: View {
    let segment: SegmentSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(segment.name)
                    .font(.headline)
                    .lineLimit(1)
                Spacer()
                Text(segment.distanceLabel)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            HStack(spacing: 8) {
                Text(segment.direction)
                Text("\(segment.attempts) attempts")
                Text(segment.slopeLabel)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }
}

struct SegmentDetailView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var viewModel: AppViewModel
    let segmentID: Int64
    let title: String
    @State private var segment: SegmentDetail?
    @State private var isLoading = false
    @State private var isEditing = false
    @State private var isConfirmingDelete = false
    @State private var toleranceMeters = 15.0
    @State private var effortSort: SegmentEffortSort = .totalTime
    @State private var effortsResponse: SegmentEffortsResponse?

    var body: some View {
        Form {
            if let segment {
                Section {
                    Text(segment.name)
                        .font(.title2)
                        .fontWeight(.semibold)
                    if let description = segment.description, !description.isEmpty {
                        Text(description)
                            .foregroundStyle(.secondary)
                    }
                    LabeledContent("Direction", value: segment.direction)
                    if let distance = segment.distanceMeters {
                        LabeledContent("Distance", value: Formatters.distance(distance))
                    }
                    if let slope = segment.slopePercent {
                        LabeledContent("Slope", value: "\(Formatters.number(slope))%")
                    }
                    if let ascent = segment.elevationGainM {
                        LabeledContent("Ascent", value: Formatters.elevation(ascent))
                    }
                    if let net = segment.netElevationM {
                        LabeledContent("Net", value: Formatters.signedElevation(net))
                    }
                }

                Section("Map") {
                    if segment.routePoints.count >= 2 {
                        ActivityRouteMap(points: segment.routePoints, paintMetric: .none)
                            .frame(height: 260)
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                    } else {
                        Text("No segment geometry stored.")
                            .foregroundStyle(.secondary)
                    }
                }

                Section("Matched Activities") {
                    Stepper(value: $toleranceMeters, in: 1...100, step: 1) {
                        LabeledContent("Tolerance", value: Formatters.elevation(toleranceMeters))
                    }

                    Picker("Sort", selection: $effortSort) {
                        ForEach(SegmentEffortSort.allCases) { sort in
                            Text(sort.title).tag(sort)
                        }
                    }

                    Button(viewModel.isLoadingSegmentEfforts ? "Finding..." : "Find Efforts") {
                        Task { await loadEfforts(refresh: false) }
                    }
                    .disabled(viewModel.isBusy)

                    Button("Refresh Cache") {
                        Task { await loadEfforts(refresh: true) }
                    }
                    .disabled(viewModel.isBusy)
                }

                if let effortsResponse {
                    Section("Effort Summary") {
                        SegmentEffortSummaryView(efforts: effortsResponse.activities)
                    }

                    Section("Efforts") {
                        if effortsResponse.activities.isEmpty {
                            Text("No same-direction efforts found for this segment.")
                                .foregroundStyle(.secondary)
                        } else {
                            ForEach(effortsResponse.activities) { effort in
                                NavigationLink {
                                    SegmentEffortDetailView(
                                        viewModel: viewModel,
                                        segmentID: segmentID,
                                        effort: effort,
                                        tolerance: effortsResponse.tolerance
                                    )
                                } label: {
                                    SegmentEffortRow(effort: effort)
                                }
                            }
                        }
                    }
                } else if viewModel.isLoadingSegmentEfforts {
                    Section {
                        ProgressView("Finding efforts...")
                    }
                }

                Section("Dates") {
                    LabeledContent("Created", value: segment.createdAt)
                    LabeledContent("Updated", value: segment.updatedAt)
                }
            } else if isLoading {
                Section {
                    ProgressView("Loading segment...")
                }
            } else {
                ContentUnavailableView(
                    "Segment Unavailable",
                    systemImage: "point.topleft.down.curvedto.point.bottomright.up"
                )
            }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if segment != nil {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button {
                        isEditing = true
                    } label: {
                        Image(systemName: "pencil")
                    }
                    .disabled(viewModel.isBusy)

                    Button(role: .destructive) {
                        isConfirmingDelete = true
                    } label: {
                        Image(systemName: "trash")
                    }
                    .disabled(viewModel.isBusy)
                }
            }
        }
        .task(id: segmentID) {
            await loadSegment()
            await loadEfforts(refresh: false)
        }
        .refreshable {
            await loadSegment()
            await loadEfforts(refresh: false)
        }
        .sheet(isPresented: $isEditing) {
            if let segment {
                SegmentMetadataEditView(viewModel: viewModel, segment: segment) { updated in
                    self.segment = updated
                }
            }
        }
        .alert("Delete Segment", isPresented: $isConfirmingDelete) {
            Button("Cancel", role: .cancel) {}
            Button("Delete", role: .destructive) {
                Task {
                    if await viewModel.deleteSegment(id: segmentID) {
                        dismiss()
                    }
                }
            }
        } message: {
            Text("This segment will be removed.")
        }
    }

    private func loadSegment() async {
        isLoading = true
        defer { isLoading = false }
        segment = await viewModel.loadSegment(id: segmentID)
    }

    private func loadEfforts(refresh: Bool) async {
        effortsResponse = await viewModel.loadSegmentEfforts(
            segmentID: segmentID,
            tolerance: toleranceMeters,
            sort: effortSort,
            refresh: refresh
        )
    }
}

struct SegmentEffortDetailView: View {
    @ObservedObject var viewModel: AppViewModel
    let segmentID: Int64
    let effort: SegmentEffort
    let tolerance: Double
    @State private var detail: SegmentEffortDetail?
    @State private var paintMetric: RoutePaintMetric = .none
    @State private var chartMetric: RoutePaintMetric = .altitude

    private var routeSnapshot: RouteSnapshot? {
        guard let detail else { return nil }
        return RouteSnapshot(source: "segment_effort", count: detail.points.count, points: detail.points)
    }

    var body: some View {
        Form {
            Section {
                Text(effort.activity.name)
                    .font(.title3)
                    .fontWeight(.semibold)
                LabeledContent("Date", value: Formatters.longDate(effort.activity.startDate))
                LabeledContent("Match", value: "\(Formatters.number(effort.overlapPercentage))%")
                LabeledContent("Tolerance", value: Formatters.elevation(tolerance))
            }

            if let detail {
                Section("Metrics") {
                    LabeledContent("Time", value: metricDuration(detail.metrics.elapsedSeconds, fallback: effort.segmentElapsedSecs))
                    LabeledContent("Distance", value: metricDistance(detail.metrics.distance, fallback: effort.segmentDistance))
                    LabeledContent("Elevation", value: metricElevation(detail.metrics.elevationGain, fallback: effort.segmentElevation))
                    LabeledContent("Avg HR", value: metricHeartRate(detail.metrics.avgHR, fallback: effort.segmentAvgHR))
                    LabeledContent("Avg Speed", value: metricSpeed(detail.metrics.avgSpeed, fallback: effort.segmentAvgSpeed))
                    LabeledContent("Points", value: "\(detail.points.count)")
                    LabeledContent("Indices", value: "\(detail.startIndex)-\(detail.endIndex)")
                }

                Section("Map") {
                    if detail.points.count >= 2 {
                        Picker("Paint", selection: $paintMetric) {
                            ForEach(RoutePaintMetric.paintMetrics) { metric in
                                Text(metric.title).tag(metric)
                            }
                        }
                        .pickerStyle(.menu)

                        VStack {
                            ActivityRouteMap(points: detail.points, paintMetric: paintMetric)
                                .frame(height: 260)
                                .clipShape(RoundedRectangle(cornerRadius: 8))
                        }
                        .frame(maxWidth: .infinity, minHeight: 260)

                        if let routeSnapshot, paintMetric != .none && !routeSnapshot.hasData(for: paintMetric) {
                            Text("\(paintMetric.title) samples are not stored for this effort.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    } else {
                        Text("No route points stored for this effort.")
                            .foregroundStyle(.secondary)
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

                Section("Activity") {
                    NavigationLink {
                        ActivityDetailView(viewModel: viewModel, activity: detail.activity)
                    } label: {
                        Label("Open Full Activity", systemImage: "figure.outdoor.cycle")
                    }
                }
            } else if viewModel.isLoadingSegmentEffortDetail {
                Section {
                    ProgressView("Loading effort...")
                }
            } else {
                ContentUnavailableView(
                    "Effort Unavailable",
                    systemImage: "point.topleft.down.curvedto.point.bottomright.up"
                )
            }
        }
        .navigationTitle("Effort")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: effort.id) {
            await loadDetail()
        }
        .refreshable {
            await loadDetail()
        }
    }

    private func loadDetail() async {
        detail = await viewModel.loadSegmentEffortDetail(
            segmentID: segmentID,
            activityID: effort.activity.id,
            tolerance: tolerance
        )
        if let routeSnapshot, !routeSnapshot.hasData(for: chartMetric) {
            chartMetric = routeSnapshot.chartMetrics.first ?? .altitude
        }
        if let routeSnapshot, paintMetric != .none && !routeSnapshot.hasData(for: paintMetric) {
            paintMetric = .none
        }
    }

    private func metricDuration(_ value: Double, fallback: Double?) -> String {
        let seconds = value > 0 ? value : (fallback ?? 0)
        guard seconds > 0 else { return "n/a" }
        return Formatters.duration(seconds)
    }

    private func metricDistance(_ value: Double, fallback: Double?) -> String {
        let meters = value > 0 ? value : (fallback ?? 0)
        guard meters > 0 else { return "n/a" }
        return Formatters.distance(meters)
    }

    private func metricElevation(_ value: Double, fallback: Double?) -> String {
        let meters = value > 0 ? value : (fallback ?? 0)
        guard meters > 0 else { return "n/a" }
        return Formatters.elevation(meters)
    }

    private func metricHeartRate(_ value: Double, fallback: Double?) -> String {
        let bpm = value > 0 ? value : (fallback ?? 0)
        guard bpm > 0 else { return "n/a" }
        return "\(Formatters.wholeNumber(bpm)) bpm"
    }

    private func metricSpeed(_ value: Double, fallback: Double?) -> String {
        let speed = value > 0 ? value : (fallback ?? 0)
        guard speed > 0 else { return "n/a" }
        return Formatters.speed(speed)
    }
}

struct SegmentEffortSummaryView: View {
    let efforts: [SegmentEffort]

    private var timedEfforts: [SegmentEffort] {
        efforts.filter { ($0.segmentElapsedSecs ?? 0) > 0 }
    }

    var body: some View {
        LabeledContent("Efforts", value: "\(efforts.count)")
        if let best = timedEfforts.compactMap(\.segmentElapsedSecs).min() {
            LabeledContent("Best", value: Formatters.duration(best))
        } else {
            LabeledContent("Best", value: "n/a")
        }
        if let avgHR {
            LabeledContent("Avg HR", value: "\(Formatters.wholeNumber(avgHR)) bpm")
        } else {
            LabeledContent("Avg HR", value: "n/a")
        }
    }

    private var avgHR: Double? {
        let values = efforts.compactMap(\.segmentAvgHR).filter { $0 > 0 }
        guard !values.isEmpty else { return nil }
        return values.reduce(0, +) / Double(values.count)
    }
}

struct SegmentEffortRow: View {
    let effort: SegmentEffort

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(effort.activity.name)
                    .font(.headline)
                    .lineLimit(1)
                Spacer()
                Text(effortTime)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 8) {
                Text(Formatters.date(effort.activity.startDate))
                Text(matchLabel)
                if let speed = effort.segmentAvgSpeed, speed > 0 {
                    Text(Formatters.speed(speed))
                }
                if let hr = effort.segmentAvgHR, hr > 0 {
                    Text("\(Formatters.wholeNumber(hr)) bpm")
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
    }

    private var effortTime: String {
        guard let seconds = effort.segmentElapsedSecs, seconds > 0 else { return "n/a" }
        return Formatters.duration(seconds)
    }

    private var matchLabel: String {
        "\(Formatters.number(effort.overlapPercentage))% match"
    }
}
