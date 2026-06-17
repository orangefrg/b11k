import CoreLocation
import SwiftUI

struct SegmentCreateView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var viewModel: AppViewModel
    let activity: Activity
    let routeSnapshot: RouteSnapshot

    @State private var name = ""
    @State private var description = ""
    @State private var startOffset = 0
    @State private var endOffset: Int

    init(viewModel: AppViewModel, activity: Activity, routeSnapshot: RouteSnapshot) {
        self.viewModel = viewModel
        self.activity = activity
        self.routeSnapshot = routeSnapshot
        _endOffset = State(initialValue: max(1, routeSnapshot.points.count - 1))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Details") {
                    TextField("Name", text: $name)
                        .textInputAutocapitalization(.words)
                    TextField("Description", text: $description, axis: .vertical)
                        .lineLimit(2...5)
                }

                Section("Range") {
                    LabeledContent("Start", value: pointLabel(startOffset))
                    Slider(
                        value: startOffsetBinding,
                        in: 0...Double(maxStartOffset),
                        step: 1
                    )

                    LabeledContent("Finish", value: pointLabel(endOffset))
                    Slider(
                        value: endOffsetBinding,
                        in: Double(minEndOffset)...Double(maxEndOffset),
                        step: 1
                    )

                    LabeledContent("Distance", value: Formatters.distance(selectedDistanceMeters))
                    LabeledContent("Points", value: "\(selectedPoints.count)")
                }

                Section("Preview") {
                    if selectedPoints.count >= 2 {
                        ActivityRouteMap(points: selectedPoints, paintMetric: .none)
                            .frame(height: 240)
                            .clipShape(RoundedRectangle(cornerRadius: 8))
                    }
                }
            }
            .navigationTitle("Create Segment")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(viewModel.isMutatingSegment ? "Saving..." : "Save") {
                        Task { await save() }
                    }
                    .disabled(isSaveDisabled)
                }
            }
        }
    }

    private var selectedPoints: [RoutePoint] {
        routeSnapshot.pointsForSegment(startOffset: startOffset, endOffset: endOffset)
    }

    private var selectedDistanceMeters: Double {
        RouteSnapshot.distanceMeters(for: selectedPoints)
    }

    private var maxStartOffset: Int {
        max(0, routeSnapshot.points.count - 2)
    }

    private var minEndOffset: Int {
        min(routeSnapshot.points.count - 1, startOffset + 1)
    }

    private var maxEndOffset: Int {
        max(1, routeSnapshot.points.count - 1)
    }

    private var isSaveDisabled: Bool {
        name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || selectedPoints.count < 2 || viewModel.isBusy
    }

    private var startOffsetBinding: Binding<Double> {
        Binding<Double>(
            get: { Double(startOffset) },
            set: { value in
                startOffset = min(max(0, Int(value.rounded())), maxStartOffset)
                endOffset = min(max(startOffset + 1, endOffset), maxEndOffset)
            }
        )
    }

    private var endOffsetBinding: Binding<Double> {
        Binding<Double>(
            get: { Double(endOffset) },
            set: { value in
                endOffset = min(max(startOffset + 1, Int(value.rounded())), maxEndOffset)
            }
        )
    }

    private func pointLabel(_ offset: Int) -> String {
        guard routeSnapshot.points.indices.contains(offset) else { return "n/a" }
        return "\(routeSnapshot.points[offset].index)"
    }

    private func save() async {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty, let first = selectedPoints.first, let last = selectedPoints.last else {
            return
        }
        let trimmedDescription = description.trimmingCharacters(in: .whitespacesAndNewlines)
        if await viewModel.createSegment(
            activityID: activity.id,
            name: trimmedName,
            description: trimmedDescription,
            startIndex: first.index,
            endIndex: last.index
        ) != nil {
            dismiss()
        }
    }
}

struct SegmentMetadataEditView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var viewModel: AppViewModel
    let segment: SegmentDetail
    let onUpdated: (SegmentDetail) -> Void

    @State private var name: String
    @State private var description: String

    init(viewModel: AppViewModel, segment: SegmentDetail, onUpdated: @escaping (SegmentDetail) -> Void) {
        self.viewModel = viewModel
        self.segment = segment
        self.onUpdated = onUpdated
        _name = State(initialValue: segment.name)
        _description = State(initialValue: segment.description ?? "")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Details") {
                    TextField("Name", text: $name)
                        .textInputAutocapitalization(.words)
                    TextField("Description", text: $description, axis: .vertical)
                        .lineLimit(2...5)
                }
            }
            .navigationTitle("Edit Segment")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(viewModel.isMutatingSegment ? "Saving..." : "Save") {
                        Task { await save() }
                    }
                    .disabled(isSaveDisabled)
                }
            }
        }
    }

    private var isSaveDisabled: Bool {
        name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || viewModel.isBusy
    }

    private func save() async {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedDescription = description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else { return }
        if let updated = await viewModel.updateSegment(id: segment.id, name: trimmedName, description: trimmedDescription) {
            onUpdated(updated)
            dismiss()
        }
    }
}

extension RouteSnapshot {
    func pointsForSegment(startOffset: Int, endOffset: Int) -> [RoutePoint] {
        guard !points.isEmpty else { return [] }
        let lower = max(0, min(startOffset, points.count - 1))
        let upper = max(lower, min(endOffset, points.count - 1))
        return Array(points[lower...upper])
    }

    static func distanceMeters(for points: [RoutePoint]) -> Double {
        guard points.count >= 2 else { return 0 }
        if let first = points.first?.cumulativeDistance, let last = points.last?.cumulativeDistance, last >= first {
            return last - first
        }

        var total = 0.0
        for index in 1..<points.count {
            let previous = CLLocation(latitude: points[index - 1].lat, longitude: points[index - 1].lng)
            let current = CLLocation(latitude: points[index].lat, longitude: points[index].lng)
            total += current.distance(from: previous)
        }
        return total
    }
}
