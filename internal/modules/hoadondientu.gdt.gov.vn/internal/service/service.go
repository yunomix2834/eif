package service

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/yunotools/eif/internal/core/apperr"
	corehttp "github.com/yunotools/eif/internal/core/protocol/httpclient"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

type service struct {
	client             client.Client
	sessions           *session.Manager
	maxQueryDays       int
	maxExportDays      int
	upstreamLimiter    *upstreamLimiter
	rateLimitRetries   int
	rateLimitBaseDelay time.Duration
	queryCache         *invoiceQueryCache
	queryMu            sync.Mutex
}

func New(
	client client.Client,
	sessions *session.Manager,
	maxQueryDays int,
	maxExportDays int,
	minRequestInterval time.Duration,
	rateLimitRetries int,
	rateLimitBaseDelay time.Duration,
	queryCacheTTL time.Duration,
) Service {
	return &service{
		client:             client,
		sessions:           sessions,
		maxQueryDays:       maxQueryDays,
		maxExportDays:      maxExportDays,
		upstreamLimiter:    newUpstreamLimiter(minRequestInterval),
		rateLimitRetries:   rateLimitRetries,
		rateLimitBaseDelay: rateLimitBaseDelay,
		queryCache:         newInvoiceQueryCache(queryCacheTTL),
	}
}

func (s *service) getAuthContext(
	sessionID string,
) (
	client.AuthenticatedContext,
	error,
) {
	sess, err := s.sessions.Get(sessionID)
	if err != nil {
		return client.AuthenticatedContext{}, apperr.New(
			apperr.CodeSessionExpired,
			err,
		)
	}

	return client.AuthenticatedContext{
		Token:   sess.Token,
		Cookies: sess.Cookies,
	}, nil
}

func (s *service) syncAuthCookies(
	sessionID string,
	auth *client.AuthenticatedContext,
) error {
	if auth == nil || len(auth.Cookies) == 0 {
		return nil
	}

	if err := s.sessions.UpdateCookies(sessionID, auth.Cookies); err != nil {
		if errors.Is(err, session.ErrNotFound) ||
			errors.Is(err, session.ErrExpired) {
			return apperr.New(
				apperr.CodeSessionExpired,
				err,
			)
		}
		return apperr.New(
			apperr.CodeInternalError,
			err,
		)
	}

	return nil
}

func (s *service) mapToAppError(err error) error {
	if err == nil {
		return nil
	}

	var netErr net.Error
	if errors.As(
		err,
		&netErr,
	) && netErr.Timeout() {
		return apperr.New(
			apperr.CodeHDDTGDTTimeout,
			err,
		)
	}

	var httpErr *corehttp.HTTPError
	if errors.As(
		err,
		&httpErr,
	) {
		if httpErr.StatusCode == http.StatusUnauthorized ||
			httpErr.StatusCode == http.StatusForbidden {
			return apperr.New(
				apperr.CodeSessionExpired,
				err,
			)
		}
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return apperr.New(
				apperr.CodeHDDTGDTRateLimited,
				err,
			)
		}
		return apperr.New(
			apperr.CodeHDDTGDTBadGateway,
			err,
		)
	}

	return apperr.New(
		apperr.CodeHDDTGDTInvalidResponse,
		err,
	)
}
