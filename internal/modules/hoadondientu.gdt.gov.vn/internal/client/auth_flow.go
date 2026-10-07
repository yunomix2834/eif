package client

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	corehttp "github.com/yunotools/eif/internal/core/protocol/httpclient"
)

var ErrAuthFlowNotFound = errors.New("authentication flow not found")

type authFlow struct {
	client    *corehttp.Client
	expiresAt time.Time
}

type authFlowStore struct {
	mu    sync.Mutex
	items map[string]*authFlow
	ttl   time.Duration
}

func newAuthFlowStore(ttl time.Duration) *authFlowStore {
	return &authFlowStore{
		items: make(map[string]*authFlow),
		ttl:   ttl,
	}
}

func newAuthFlowID() (
	string,
	error,
) {
	buffer := make(
		[]byte,
		24,
	)

	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return hex.EncodeToString(buffer), nil
}

func (s *authFlowStore) create(client *corehttp.Client) (
	string,
	error,
) {
	id, err := newAuthFlowID()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	s.items[id] = &authFlow{
		client:    client,
		expiresAt: time.Now().Add(s.ttl),
	}

	return id, nil
}

func (s *authFlowStore) consume(id string) (
	*authFlow,
	error,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	flow, ok := s.items[id]
	if !ok {
		return nil, ErrAuthFlowNotFound
	}

	delete(
		s.items,
		id,
	)

	return flow, nil
}

func (s *authFlowStore) cleanupLocked() {
	now := time.Now()

	for id, flow := range s.items {
		if now.After(flow.expiresAt) {
			delete(
				s.items,
				id,
			)
		}
	}
}
