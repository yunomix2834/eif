package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type jwtPayload struct {
	Exp int64 `json:"exp"`
}

func ParseJWTExpiry(token string) (
	time.Time,
	error,
) {
	parts := strings.Split(
		token,
		".",
	)

	// JWT có 3 phần
	// parts[0] = Header
	// parts[1] = Payload
	// parts[2] = Signature
	if len(parts) != 3 {
		return time.Time{}, errors.New("invalid jwt format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}

	var data jwtPayload
	if err := json.Unmarshal(
		payload,
		&data,
	); err != nil {
		return time.Time{}, err
	}

	if data.Exp <= 0 {
		return time.Time{}, errors.New("jwt exp is missing")
	}

	return time.Unix(
		data.Exp,
		0,
	), nil
}
