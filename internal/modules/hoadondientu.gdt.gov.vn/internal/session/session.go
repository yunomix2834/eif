package session

import "time"

type Session struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Token     string    `json:"token"`
	Cookies   []Cookie  `json:"cookies"`
	ExpiredAt time.Time `json:"expired_at"`
}
