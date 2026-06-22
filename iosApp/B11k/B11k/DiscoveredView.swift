import MapKit
import SwiftUI
import UIKit

struct DiscoveredView: View {
    @ObservedObject var viewModel: AppViewModel

    var body: some View {
        List {
            if !viewModel.isAuthorized {
                ContentUnavailableView(
                    "Connect Strava",
                    systemImage: "map",
                    description: Text("Use Settings to connect to B11K.")
                )
            } else if let status = viewModel.discoveredStatus {
                Section("Coverage") {
                    LabeledContent("Status", value: status.statusLabel)
                    LabeledContent("Buildable Activities", value: "\(status.buildableActivities)")
                    LabeledContent("Cached Activities", value: "\(status.cachedActivities)")
                    LabeledContent("Reveal Radius", value: Formatters.elevation(status.radiusMeters))
                    LabeledContent("Sample Distance", value: Formatters.elevation(status.sampleDistanceMeters))
                    if let rebuiltAt = status.rebuiltAt {
                        LabeledContent("Rebuilt", value: Formatters.longDate(rebuiltAt))
                    }
                    if let message = status.message, !message.isEmpty {
                        Text(message)
                            .font(.caption)
                            .foregroundStyle(status.stale ? .orange : .secondary)
                    }
                }

                if status.bbox != nil {
                    Section("Map") {
                        if let snapshot = viewModel.discoveredMapSnapshot {
                            DiscoveredCoverageMap(snapshot: snapshot)
                                .frame(height: 320)
                                .clipShape(RoundedRectangle(cornerRadius: 8))

                            Button(viewModel.isLoadingDiscoveredMap ? "Refreshing Map..." : "Refresh Map") {
                                Task { await viewModel.loadDiscoveredMap() }
                            }
                            .disabled(viewModel.isBusy)
                        } else if viewModel.isLoadingDiscoveredMap {
                            ProgressView("Loading map...")
                                .frame(maxWidth: .infinity, minHeight: 160)
                        } else {
                            Button("Load Map") {
                                Task { await viewModel.loadDiscoveredMap() }
                            }
                            .disabled(viewModel.isBusy)
                        }
                    }
                }

                if let bbox = status.bbox, bbox.count == 4 {
                    Section("Bounds") {
                        LabeledContent("West", value: Formatters.coordinate(bbox[0]))
                        LabeledContent("South", value: Formatters.coordinate(bbox[1]))
                        LabeledContent("East", value: Formatters.coordinate(bbox[2]))
                        LabeledContent("North", value: Formatters.coordinate(bbox[3]))
                    }
                }

                Section {
                    Button(viewModel.isRebuildingDiscovered ? "Rebuilding..." : "Rebuild Coverage") {
                        Task { await viewModel.rebuildDiscoveredCoverage() }
                    }
                    .disabled(viewModel.isBusy)
                }
            } else if viewModel.isLoadingDiscovered {
                Section {
                    ProgressView("Loading coverage...")
                }
            } else {
                ContentUnavailableView(
                    "No Coverage",
                    systemImage: "map",
                    description: Text("Sync activities before opening discovered coverage.")
                )
            }
        }
        .task {
            if viewModel.isAuthorized && viewModel.discoveredStatus == nil {
                await viewModel.loadDiscoveredStatus()
            }
        }
        .task(id: viewModel.discoveredStatus?.mapCacheKey) {
            guard viewModel.isAuthorized,
                  viewModel.discoveredStatus?.bbox != nil,
                  viewModel.discoveredMapSnapshot == nil else {
                return
            }
            await viewModel.loadDiscoveredMap()
        }
        .refreshable {
            await viewModel.loadDiscoveredStatus()
        }
        .overlay {
            if viewModel.isLoadingDiscovered && viewModel.discoveredStatus != nil {
                ProgressView("Loading coverage...")
            }
        }
    }
}

struct DiscoveredCoverageMap: UIViewRepresentable {
    let snapshot: DiscoveredMapSnapshot

    func makeUIView(context: Context) -> MKMapView {
        let mapView = MKMapView()
        mapView.delegate = context.coordinator
        mapView.pointOfInterestFilter = .excludingAll
        mapView.showsCompass = false
        mapView.isRotateEnabled = false
        return mapView
    }

    func updateUIView(_ mapView: MKMapView, context: Context) {
        context.coordinator.render(snapshot: snapshot, on: mapView)
    }

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    final class Coordinator: NSObject, MKMapViewDelegate {
        private var renderedKey = ""
        private var fogOverlayIDs = Set<ObjectIdentifier>()
        private var coverageOverlayIDs = Set<ObjectIdentifier>()

        func render(snapshot: DiscoveredMapSnapshot, on mapView: MKMapView) {
            guard snapshot.renderKey != renderedKey || mapView.overlays.isEmpty else { return }

            let fogOverlays = decodeOverlays(from: snapshot.fogGeoJSON)
            let coverageOverlays = decodeOverlays(from: snapshot.coverageGeoJSON)

            fogOverlayIDs = overlayIDs(fogOverlays)
            coverageOverlayIDs = overlayIDs(coverageOverlays)

            mapView.removeOverlays(mapView.overlays)
            mapView.addOverlays(fogOverlays)
            mapView.addOverlays(coverageOverlays)

            if let rect = mapRect(for: snapshot.requestBBox) {
                mapView.setVisibleMapRect(
                    rect,
                    edgePadding: UIEdgeInsets(top: 24, left: 24, bottom: 24, right: 24),
                    animated: false
                )
            }
            renderedKey = snapshot.renderKey
        }

        func mapView(_ mapView: MKMapView, rendererFor overlay: MKOverlay) -> MKOverlayRenderer {
            let overlayID = ObjectIdentifier(overlay as AnyObject)
            if coverageOverlayIDs.contains(overlayID) {
                return renderer(
                    for: overlay,
                    fillColor: UIColor.systemTeal.withAlphaComponent(0.22),
                    strokeColor: UIColor.systemTeal.withAlphaComponent(0.9),
                    lineWidth: 2
                )
            }
            if fogOverlayIDs.contains(overlayID) {
                return renderer(
                    for: overlay,
                    fillColor: UIColor.systemGray.withAlphaComponent(0.32),
                    strokeColor: UIColor.systemGray.withAlphaComponent(0.12),
                    lineWidth: 0.5
                )
            }
            return renderer(
                for: overlay,
                fillColor: UIColor.systemOrange.withAlphaComponent(0.2),
                strokeColor: UIColor.systemOrange,
                lineWidth: 2
            )
        }

        private func decodeOverlays(from data: Data) -> [MKOverlay] {
            guard !data.isEmpty else { return [] }
            do {
                return try MKGeoJSONDecoder()
                    .decode(data)
                    .flatMap(overlays(from:))
            } catch {
                return []
            }
        }

        private func overlays(from object: MKGeoJSONObject) -> [MKOverlay] {
            if let feature = object as? MKGeoJSONFeature {
                return feature.geometry.compactMap { $0 as? MKOverlay }
            }
            if let overlay = object as? MKOverlay {
                return [overlay]
            }
            return []
        }

        private func overlayIDs(_ overlays: [MKOverlay]) -> Set<ObjectIdentifier> {
            Set(overlays.map { ObjectIdentifier($0 as AnyObject) })
        }

        private func renderer(
            for overlay: MKOverlay,
            fillColor: UIColor,
            strokeColor: UIColor,
            lineWidth: CGFloat
        ) -> MKOverlayRenderer {
            if let multiPolygon = overlay as? MKMultiPolygon {
                let renderer = MKMultiPolygonRenderer(multiPolygon: multiPolygon)
                renderer.fillColor = fillColor
                renderer.strokeColor = strokeColor
                renderer.lineWidth = lineWidth
                return renderer
            }
            if let polygon = overlay as? MKPolygon {
                let renderer = MKPolygonRenderer(polygon: polygon)
                renderer.fillColor = fillColor
                renderer.strokeColor = strokeColor
                renderer.lineWidth = lineWidth
                return renderer
            }
            if let polyline = overlay as? MKPolyline {
                let renderer = MKPolylineRenderer(polyline: polyline)
                renderer.strokeColor = strokeColor
                renderer.lineWidth = max(2, lineWidth)
                renderer.lineJoin = .round
                renderer.lineCap = .round
                return renderer
            }
            return MKOverlayRenderer(overlay: overlay)
        }

        private func mapRect(for bbox: [Double]) -> MKMapRect? {
            guard bbox.count == 4 else { return nil }
            let minLng = bbox[0]
            let minLat = bbox[1]
            let maxLng = bbox[2]
            let maxLat = bbox[3]
            guard minLng.isFinite, minLat.isFinite, maxLng.isFinite, maxLat.isFinite else { return nil }

            let coordinates = [
                CLLocationCoordinate2D(latitude: minLat, longitude: minLng),
                CLLocationCoordinate2D(latitude: minLat, longitude: maxLng),
                CLLocationCoordinate2D(latitude: maxLat, longitude: minLng),
                CLLocationCoordinate2D(latitude: maxLat, longitude: maxLng)
            ]

            var rect = MKMapRect.null
            for coordinate in coordinates {
                let point = MKMapPoint(coordinate)
                let pointRect = MKMapRect(x: point.x, y: point.y, width: 1, height: 1)
                rect = rect.union(pointRect)
            }
            return rect.isNull || rect.isEmpty ? nil : rect
        }
    }
}
