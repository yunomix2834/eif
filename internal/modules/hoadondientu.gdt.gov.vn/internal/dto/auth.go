package dto

import "time"

type AuthenticationRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	CValue   string `json:"cvalue"`
	CKey     string `json:"ckey"`

	FlowID string `json:"flow_id"`
}

type UpstreamAuthenticationRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	CValue   string `json:"cvalue"`
	CKey     string `json:"ckey"`
}

type AuthenticationTokenResponse struct {
	Token string `json:"token"`
}

type AuthenticationResponse struct {
	SessionID string    `json:"session_id"`
	ExpiredAt time.Time `json:"expired_at"`
}
