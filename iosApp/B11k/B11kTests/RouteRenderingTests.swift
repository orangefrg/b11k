import MapKit
import Testing
@testable import B11k

@MainActor
struct RouteRenderingTests {
    private func render(_ points: [RoutePoint], metric: RoutePaintMetric) -> (MKMapView, ActivityRouteMap.Coordinator) {
        let map = MKMapView(frame: CGRect(x: 0, y: 0, width: 400, height: 300))
        let coordinator = ActivityRouteMap.Coordinator()
        coordinator.update(points: points, paintMetric: metric)
        coordinator.renderIfNeeded(on: map)
        return (map, coordinator)
    }

    @Test func coloredLongRouteIncludesTheFinishAfterDownsampling() throws {
        let points = (0..<400).map { index in
            RoutePoint(index: index, lat: 44 + Double(index) * 0.0001,
                       lng: 20 + Double(index) * 0.0001, speed: Double(index))
        }
        let (map, _) = render(points, metric: .speed)
        let last = try #require(map.overlays.last as? MKPolyline)
        let finish = last.points()[last.pointCount - 1].coordinate
        #expect(abs(finish.latitude - points.last!.lat) < 0.000001)
        #expect(abs(finish.longitude - points.last!.lng) < 0.000001)
        #expect(map.overlays.count <= 200)
    }

    @Test func repairedRouteWithSameEndpointsReplacesOldGeometry() throws {
        let points = [RoutePoint(index: 0, lat: 44, lng: 20), RoutePoint(index: 1, lat: 44.01, lng: 20.01), RoutePoint(index: 2, lat: 44.02, lng: 20.02)]
        let (map, coordinator) = render(points, metric: .none)
        var repaired = points
        repaired[1] = RoutePoint(index: 1, lat: 44.015, lng: 20.012)
        coordinator.update(points: repaired, paintMetric: .none)
        coordinator.renderIfNeeded(on: map)
        let line = try #require(map.overlays.first as? MKPolyline)
        #expect(map.overlays.count == 1)
        #expect(abs(line.points()[1].coordinate.latitude - 44.015) < 0.000001)
    }

    @Test func absentMetricFallsBackToRouteAndInvalidCoordinatesDoNotReachMapKit() throws {
        let points = [RoutePoint(index: 0, lat: 44, lng: 20), RoutePoint(index: 1, lat: 999, lng: 999), RoutePoint(index: 2, lat: 44.02, lng: 20.02)]
        let (map, coordinator) = render(points, metric: .watts)
        let line = try #require(map.overlays.first as? MKPolyline)
        #expect(map.overlays.count == 1)
        #expect(line.pointCount == 2)
        coordinator.update(points: [points[1]], paintMetric: .watts)
        coordinator.renderIfNeeded(on: map)
        #expect(map.overlays.isEmpty)
    }
}
