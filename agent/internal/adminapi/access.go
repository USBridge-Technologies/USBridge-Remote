package adminapi

import (
	"net/http"

	"usbridge_agent/internal/guestauth"
)

type accessCreateReq struct {
	AdminPIN       string           `json:"admin_pin"`
	Label          string           `json:"label"`
	Policy         guestauth.Policy `json:"policy"`
	ExpiresInSec   int64            `json:"expires_in_sec"`
	RequireConsent bool             `json:"require_consent"`
}

type accessCreateResp struct {
	Share *guestauth.Share `json:"share"`
	Token string           `json:"token"`
}

type accessUpdateReq struct {
	AdminPIN       string           `json:"admin_pin"`
	ID             string           `json:"id"`
	Label          string           `json:"label"`
	Policy         guestauth.Policy `json:"policy"`
	RequireConsent bool             `json:"require_consent"`
	ExpiresAt      int64            `json:"expires_at"`
}

type accessIDReq struct {
	AdminPIN string `json:"admin_pin"`
	ID       string `json:"id"`
}

type adminPINStatusResp struct {
	Configured          bool `json:"configured"`
	AlwaysRequire       bool `json:"always_require"`
	DisableMasterRemote bool `json:"disable_master_remote"`
}

type setAdminPINReq struct {
	CurrentPIN string `json:"current_pin"`
	NewPIN     string `json:"new_pin"`
}

type setSecurityOptsReq struct {
	AdminPIN               string `json:"admin_pin"`
	AlwaysRequirePIN       bool   `json:"always_require_pin"`
	DisableMasterKeyRemote bool   `json:"disable_master_key_remote"`
}

func (s *Server) registerAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /token/access/list", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.token.ListAccessTokens())
	})
	mux.HandleFunc("POST /token/access/create", func(w http.ResponseWriter, r *http.Request) {
		var req accessCreateReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		sh, tok, err := s.token.CreateLocalToken(req.AdminPIN, req.Label, req.Policy, req.ExpiresInSec, req.RequireConsent)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, accessCreateResp{Share: sh, Token: tok})
	})
	mux.HandleFunc("POST /token/access/update", func(w http.ResponseWriter, r *http.Request) {
		var req accessUpdateReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		sh, err := s.token.UpdateAccessPolicy(req.AdminPIN, req.ID, req.Label, req.Policy, req.RequireConsent, req.ExpiresAt)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sh)
	})
	mux.HandleFunc("POST /token/access/revoke", func(w http.ResponseWriter, r *http.Request) {
		var req accessIDReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		if err := s.token.RevokeAccessToken(req.AdminPIN, req.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("POST /token/access/kick", func(w http.ResponseWriter, r *http.Request) {
		var req accessIDReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		if err := s.token.KickAccessSession(req.AdminPIN, req.ID); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("GET /token/access/active", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.token.ActiveRestrictedSessions())
	})
	mux.HandleFunc("GET /token/security/status", func(w http.ResponseWriter, r *http.Request) {
		cfg, always, noMaster := s.token.AdminPINStatus()
		writeJSON(w, http.StatusOK, adminPINStatusResp{
			Configured:          cfg,
			AlwaysRequire:       always,
			DisableMasterRemote: noMaster,
		})
	})
	mux.HandleFunc("POST /token/security/verify-pin", func(w http.ResponseWriter, r *http.Request) {
		var req stringBody
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, boolBody{Value: s.token.VerifyAdminPIN(req.Value)})
	})
	mux.HandleFunc("POST /token/security/set-pin", func(w http.ResponseWriter, r *http.Request) {
		var req setAdminPINReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		if err := s.token.SetAdminPIN(req.CurrentPIN, req.NewPIN); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("POST /token/security/options", func(w http.ResponseWriter, r *http.Request) {
		var req setSecurityOptsReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		if err := s.token.SetSecurityOptions(req.AdminPIN, req.AlwaysRequirePIN, req.DisableMasterKeyRemote); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct{}{})
	})
}

func (c *Client) ListAccessTokens() []guestauth.Share {
	var out []guestauth.Share
	_ = c.do(http.MethodGet, "/token/access/list", nil, &out)
	return out
}

func (c *Client) CreateLocalToken(adminPIN, label string, policy guestauth.Policy, expiresInSec int64, requireConsent bool) (*guestauth.Share, string, error) {
	var resp accessCreateResp
	err := c.do(http.MethodPost, "/token/access/create", accessCreateReq{
		AdminPIN:       adminPIN,
		Label:          label,
		Policy:         policy,
		ExpiresInSec:   expiresInSec,
		RequireConsent: requireConsent,
	}, &resp)
	return resp.Share, resp.Token, err
}

func (c *Client) UpdateAccessPolicy(adminPIN, id, label string, policy guestauth.Policy, requireConsent bool, expiresAt int64) (*guestauth.Share, error) {
	var sh guestauth.Share
	err := c.do(http.MethodPost, "/token/access/update", accessUpdateReq{
		AdminPIN:       adminPIN,
		ID:             id,
		Label:          label,
		Policy:         policy,
		RequireConsent: requireConsent,
		ExpiresAt:      expiresAt,
	}, &sh)
	if err != nil {
		return nil, err
	}
	return &sh, nil
}

func (c *Client) RevokeAccessToken(adminPIN, id string) error {
	return c.do(http.MethodPost, "/token/access/revoke", accessIDReq{AdminPIN: adminPIN, ID: id}, nil)
}

func (c *Client) KickAccessSession(adminPIN, id string) error {
	return c.do(http.MethodPost, "/token/access/kick", accessIDReq{AdminPIN: adminPIN, ID: id}, nil)
}

func (c *Client) ActiveRestrictedSessions() []guestauth.Session {
	var out []guestauth.Session
	_ = c.do(http.MethodGet, "/token/access/active", nil, &out)
	return out
}

func (c *Client) AdminPINStatus() (configured, alwaysRequire, disableMasterRemote bool) {
	var resp adminPINStatusResp
	_ = c.do(http.MethodGet, "/token/security/status", nil, &resp)
	return resp.Configured, resp.AlwaysRequire, resp.DisableMasterRemote
}

func (c *Client) VerifyAdminPIN(pin string) bool {
	var resp boolBody
	_ = c.do(http.MethodPost, "/token/security/verify-pin", stringBody{Value: pin}, &resp)
	return resp.Value
}

func (c *Client) SetAdminPIN(currentPIN, newPIN string) error {
	return c.do(http.MethodPost, "/token/security/set-pin", setAdminPINReq{CurrentPIN: currentPIN, NewPIN: newPIN}, nil)
}

func (c *Client) SetSecurityOptions(adminPIN string, alwaysRequirePIN, disableMasterKeyRemote bool) error {
	return c.do(http.MethodPost, "/token/security/options", setSecurityOptsReq{
		AdminPIN:               adminPIN,
		AlwaysRequirePIN:       alwaysRequirePIN,
		DisableMasterKeyRemote: disableMasterKeyRemote,
	}, nil)
}
