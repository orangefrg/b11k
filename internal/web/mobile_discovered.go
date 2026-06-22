package web

import (
	"net/http"
	"strings"
)

func (s *server) handleMobileDiscovered(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DiscoveredMapEnabled {
		http.NotFound(w, r)
		return
	}

	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	scope := s.mobileScopeFromSession(session)
	if scope.AthleteID == 0 {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile/discovered/"), "/")
	switch action {
	case "status":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status, err := s.discoveredCoverageStatus(scope.AthleteID)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, status)
	case "rebuild":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status, err := s.rebuildDiscoveredCoverage(scope.AthleteID)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, status)
	case "fog":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		minLng, minLat, maxLng, maxLat, ok := parseBBox(r.URL.Query().Get("bbox"))
		if !ok {
			http.Error(w, "bbox must be minLng,minLat,maxLng,maxLat", http.StatusBadRequest)
			return
		}
		featureCollection, err := s.discoveredFogFeatureCollection(scope.AthleteID, minLng, minLat, maxLng, maxLat)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(featureCollection))
	case "coverage":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		minLng, minLat, maxLng, maxLat, ok := parseBBox(r.URL.Query().Get("bbox"))
		if !ok {
			http.Error(w, "bbox must be minLng,minLat,maxLng,maxLat", http.StatusBadRequest)
			return
		}
		featureCollection, err := s.discoveredCoverageFeatureCollection(scope.AthleteID, minLng, minLat, maxLng, maxLat)
		if err != nil {
			s.handleDBPageError(w, r, err, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(featureCollection))
	default:
		http.NotFound(w, r)
	}
}
