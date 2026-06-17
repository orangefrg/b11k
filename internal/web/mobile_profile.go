package web

import (
	"net/http"
)

func (s *server) handleMobileProfile(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	scope := s.mobileScopeFromSession(session)
	data, err := s.buildProfileData(scope)
	if err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

func (s *server) handleMobileLogout(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mobileSessionFromRequest(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mobileMu.Lock()
	delete(s.mobileSessions, session.SessionToken)
	s.mobileMu.Unlock()

	if err := s.deleteMobileSession(session.SessionToken); err != nil {
		s.handleDBPageError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"logged_out": true})
}
