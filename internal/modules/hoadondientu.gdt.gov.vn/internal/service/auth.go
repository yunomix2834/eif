package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/yunomix2834/eif/internal/core/apperr"
	corehttp "github.com/yunomix2834/eif/internal/core/protocol/httpclient"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

func (s *service) GetCaptcha(
	ctx context.Context,
) (
	*dto.CaptchaResponse,
	error,
) {
	response, err := s.client.GetCaptcha(ctx)
	if err != nil {
		return nil, s.mapToAppError(err)
	}

	return response, nil
}

func (s *service) Authenticate(
	ctx context.Context,
	req *dto.AuthenticationRequest,
) (
	*dto.AuthenticationResponse,
	error,
) {
	if req == nil ||
		req.Username == "" ||
		req.Password == "" ||
		req.CValue == "" ||
		req.CKey == "" ||
		req.FlowID == "" {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			errors.New("username, password, cvalue, ckey and flow_id are required"),
		)
	}

	result, err := s.client.Authenticate(
		ctx,
		req,
	)
	if err != nil {
		return nil, getAuthenticationError(
			err,
			false,
		)
	}

	token, expiredAt, err := getAuthenticationToken(
		&dto.AuthenticationTokenResponse{
			Token: result.Token,
		},
	)
	if err != nil {
		return nil, err
	}

	created, err := s.sessions.Create(
		req.Username,
		token,
		result.Cookies,
		expiredAt,
	)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeInternalError,
			err,
		)
	}

	return &dto.AuthenticationResponse{
		SessionID: created.ID,
		ExpiredAt: created.ExpiredAt,
	}, nil
}

func (s *service) GetSession(sessionID string) (
	*dto.SessionResponse,
	error,
) {
	sess, err := s.sessions.Get(sessionID)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeSessionExpired,
			err,
		)
	}

	return getSessionResponse(sess), nil
}

func (s *service) RefreshSession(
	ctx context.Context,
	sessionID string,
	req *dto.SessionRefreshRequest,
) (
	*dto.SessionResponse,
	error,
) {
	if req == nil ||
		req.Password == "" ||
		req.CValue == "" ||
		req.CKey == "" ||
		req.FlowID == "" {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			errors.New("password, cvalue, ckey and flow_id are required"),
		)
	}

	current, err := s.sessions.Get(sessionID)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeSessionExpired,
			err,
		)
	}

	result, err := s.client.Authenticate(
		ctx,
		&dto.AuthenticationRequest{
			Username: current.Username,
			Password: req.Password,
			CValue:   req.CValue,
			CKey:     req.CKey,
			FlowID:   req.FlowID,
		},
	)
	if err != nil {
		return nil, getAuthenticationError(
			err,
			true,
		)
	}

	_, expiredAt, err := getAuthenticationToken(
		&dto.AuthenticationTokenResponse{Token: result.Token},
	)
	if err != nil {
		return nil, err
	}

	refreshed, err := s.sessions.Refresh(
		current.ID,
		result.Token,
		result.Cookies,
		expiredAt,
	)
	if err != nil {
		if errors.Is(
			err,
			session.ErrNotFound,
		) || errors.Is(
			err,
			session.ErrExpired,
		) {
			return nil, apperr.New(
				apperr.CodeSessionExpired,
				err,
			)
		}
		return nil, apperr.New(
			apperr.CodeInternalError,
			err,
		)
	}

	return getSessionResponse(refreshed), nil
}

func (s *service) DeleteSession(sessionID string) error {
	if sessionID == "" {
		return nil
	}

	if err := s.sessions.Delete(sessionID); err != nil {
		return apperr.New(
			apperr.CodeInternalError,
			err,
		)
	}

	return nil
}

func getAuthenticationToken(
	response *dto.AuthenticationTokenResponse,
) (
	string,
	time.Time,
	error,
) {
	if response == nil || response.Token == "" {
		return "", time.Time{}, apperr.New(
			apperr.CodeHDDTGDTInvalidResponse,
			errors.New("authenticate response does not contain token"),
		)
	}

	expiredAt, err := session.ParseJWTExpiry(response.Token)
	if err != nil {
		return "", time.Time{}, apperr.New(
			apperr.CodeHDDTGDTInvalidResponse,
			err,
		)
	}

	return response.Token, expiredAt, nil
}

func getAuthenticationError(
	err error,
	isRefreshing bool,
) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, client.ErrAuthFlowNotFound) {
		return apperr.New(
			apperr.CodeHDDTGDTCaptchaExpired,
			err,
		)
	}

	var stageErr *client.AuthenticationStageError
	if errors.As(err, &stageErr) &&
		stageErr.Stage == client.AuthenticationStageProfile {
		return apperr.New(
			apperr.CodeHDDTGDTProfileFailed,
			err,
		)
	}

	var netErr net.Error
	if errors.As(
		err,
		&netErr,
	) {
		if netErr.Timeout() {
			return apperr.New(
				apperr.CodeHDDTGDTTimeout,
				err,
			)
		}
		return apperr.New(
			apperr.CodeHDDTGDTBadGateway,
			err,
		)
	}

	var httpErr *corehttp.HTTPError
	if errors.As(
		err,
		&httpErr,
	) {
		if httpErr.StatusCode == http.StatusBadRequest ||
			httpErr.StatusCode == http.StatusUnauthorized ||
			httpErr.StatusCode == http.StatusForbidden {
			if isRefreshing {
				return apperr.New(
					apperr.CodeHDDTGDTRefreshFailed,
					err,
				)
			}
			return apperr.New(
				apperr.CodeHDDTGDTAuthenticationFailed,
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

func getSessionResponse(sess *session.Session) *dto.SessionResponse {
	remaining := time.Until(sess.ExpiredAt)
	remainingSeconds := int64(0)
	if remaining > 0 {
		remainingSeconds = int64(
			(remaining + time.Second - 1) / time.Second,
		)
	}

	return &dto.SessionResponse{
		SessionID:        sess.ID,
		Username:         sess.Username,
		ExpiredAt:        sess.ExpiredAt,
		RemainingSeconds: remainingSeconds,
	}
}
