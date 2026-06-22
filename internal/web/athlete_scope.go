package web

import (
	"errors"
	"fmt"
	"net/http"

	"b11k/internal/pggeo"
	"b11k/internal/strava"

	"github.com/jackc/pgx/v5"
)

var (
	errForbidden              = errors.New("forbidden")
	errActivitySamplesMissing = errors.New("activity samples missing")
	errSegmentIndexOutOfRange = errors.New("segment index out of range")
)

type athleteScope struct {
	AthleteID   int64
	Athlete     *strava.Athlete
	StravaToken string
}

func (s *server) webScopeFromRequest(w http.ResponseWriter, r *http.Request) (athleteScope, bool) {
	s.ensureSessionFromRequest(r)
	if s.user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return athleteScope{}, false
	}
	return athleteScope{
		AthleteID:   s.user.ID,
		Athlete:     s.user,
		StravaToken: s.token,
	}, true
}

func (s *server) mobileScopeFromSession(session mobileSession) athleteScope {
	scope := athleteScope{
		StravaToken: session.Token,
		Athlete:     session.Athlete,
	}
	if session.Athlete != nil {
		scope.AthleteID = session.Athlete.ID
	}
	return scope
}

func (s *server) listFavoriteSegments(athleteID int64) ([]pggeo.FavoriteSegment, error) {
	var segments []pggeo.FavoriteSegment
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segments, dbErr = pggeo.ListFavoriteSegments(s.ctx, conn, athleteID)
		return dbErr
	})
	return segments, err
}

func (s *server) listSegmentDashboardSummaries(athleteID int64, toleranceMeters float64) ([]pggeo.SegmentDashboardSummary, error) {
	var segments []pggeo.SegmentDashboardSummary
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segments, dbErr = pggeo.ListSegmentDashboardSummaries(s.ctx, conn, athleteID, toleranceMeters)
		return dbErr
	})
	return segments, err
}

func (s *server) getOwnedFavoriteSegment(athleteID, segmentID int64) (*pggeo.FavoriteSegment, error) {
	var segment *pggeo.FavoriteSegment
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segment, dbErr = pggeo.GetFavoriteSegment(s.ctx, conn, segmentID)
		return dbErr
	})
	if err != nil {
		return nil, err
	}
	if segment.AthleteID != athleteID {
		return nil, errForbidden
	}
	return segment, nil
}

func (s *server) createFavoriteSegmentFromActivityRange(athleteID, activityID int64, name, description string, startIndex, endIndex int) (*pggeo.FavoriteSegment, error) {
	var samples []pggeo.PointSample
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		samples, dbErr = pggeo.GetPointSamplesForActivity(s.ctx, conn, athleteID, activityID)
		return dbErr
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errActivitySamplesMissing, err)
	}
	if startIndex >= len(samples) || endIndex > len(samples) {
		return nil, errSegmentIndexOutOfRange
	}

	latLngData := make([][]float64, 0, endIndex-startIndex)
	segmentSamples := make([]pggeo.PointSample, 0, endIndex-startIndex)
	for i := startIndex; i < endIndex; i++ {
		latLngData = append(latLngData, []float64{samples[i].Lat, samples[i].Lng})
		segmentSamples = append(segmentSamples, samples[i])
	}

	var segment *pggeo.FavoriteSegment
	err = s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segment, dbErr = pggeo.InsertFavoriteSegment(s.ctx, conn, athleteID, name, description, latLngData, segmentSamples)
		return dbErr
	})
	return segment, err
}

func (s *server) createFavoriteSegmentFromPoints(athleteID int64, name, description string, latLngData [][]float64) (*pggeo.FavoriteSegment, error) {
	var segment *pggeo.FavoriteSegment
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segment, dbErr = pggeo.InsertFavoriteSegment(s.ctx, conn, athleteID, name, description, latLngData, nil)
		return dbErr
	})
	return segment, err
}

func (s *server) updateOwnedFavoriteSegment(athleteID, segmentID int64, name, description string, latLngData [][]float64) (*pggeo.FavoriteSegment, error) {
	if _, err := s.getOwnedFavoriteSegment(athleteID, segmentID); err != nil {
		return nil, err
	}
	var segment *pggeo.FavoriteSegment
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		segment, dbErr = pggeo.UpdateFavoriteSegment(s.ctx, conn, segmentID, name, description, latLngData)
		return dbErr
	})
	return segment, err
}

func (s *server) discoveredCoverageStatus(athleteID int64) (*pggeo.DiscoveredCoverageStatus, error) {
	var status *pggeo.DiscoveredCoverageStatus
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		status, dbErr = pggeo.GetDiscoveredCoverageStatus(s.ctx, conn, athleteID, s.cfg.DiscoveredSampleDistanceMeters, s.cfg.DiscoveredRevealRadiusMeters)
		return dbErr
	})
	return status, err
}

func (s *server) rebuildDiscoveredCoverage(athleteID int64) (*pggeo.DiscoveredCoverageStatus, error) {
	var status *pggeo.DiscoveredCoverageStatus
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		status, dbErr = pggeo.RebuildDiscoveredCoverage(s.ctx, conn, athleteID, s.cfg.DiscoveredSampleDistanceMeters, s.cfg.DiscoveredRevealRadiusMeters)
		return dbErr
	})
	return status, err
}

func (s *server) discoveredFogFeatureCollection(athleteID int64, minLng, minLat, maxLng, maxLat float64) (string, error) {
	var featureCollection string
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		featureCollection, dbErr = pggeo.GetDiscoveredFogFeatureCollection(s.ctx, conn, athleteID, minLng, minLat, maxLng, maxLat, s.cfg.DiscoveredSampleDistanceMeters, s.cfg.DiscoveredRevealRadiusMeters)
		return dbErr
	})
	return featureCollection, err
}

func (s *server) discoveredCoverageFeatureCollection(athleteID int64, minLng, minLat, maxLng, maxLat float64) (string, error) {
	var featureCollection string
	err := s.withDB(func(conn *pgx.Conn) error {
		var dbErr error
		featureCollection, dbErr = pggeo.GetDiscoveredCoverageFeatureCollection(s.ctx, conn, athleteID, minLng, minLat, maxLng, maxLat, s.cfg.DiscoveredSampleDistanceMeters, s.cfg.DiscoveredRevealRadiusMeters)
		return dbErr
	})
	return featureCollection, err
}
