package api

import (
	"encoding/json"
	"net/http"
)

// WebRTCOfferRequest is the body of POST /api/webrtc/offer.
type WebRTCOfferRequest struct {
	SessionID string `json:"session_id"`
	SDP       string `json:"sdp"`
}

// WebRTCOfferResponse is returned by POST /api/webrtc/offer.
type WebRTCOfferResponse struct {
	SDP string `json:"sdp"`
}

func (s *Server) webrtcOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.fail(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}

	var req WebRTCOfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	if req.SessionID == "" || req.SDP == "" {
		s.fail(w, http.StatusBadRequest, "invalid_request", nil)
		return
	}

	answer, err := s.app.WebRTCOffer(req.SessionID, req.SDP)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "webrtc_offer_failed", err)
		return
	}

	s.ok(w, "webrtc_answer", WebRTCOfferResponse{SDP: answer})
}
