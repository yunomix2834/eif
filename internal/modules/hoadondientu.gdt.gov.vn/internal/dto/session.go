package dto

import "time"

type SessionResponse struct {
	SessionID        string    `json:"session_id"`
	Username         string    `json:"username"`
	ExpiredAt        time.Time `json:"expired_at"`
	RemainingSeconds int64     `json:"remaining_seconds"`
}

type SessionRefreshRequest struct {
	Password string `json:"password"`
	CValue   string `json:"cvalue"`
	CKey     string `json:"ckey"`
	FlowID   string `json:"flow_id"`
}
