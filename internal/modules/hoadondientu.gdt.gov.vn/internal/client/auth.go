package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

func (c *hddtgdtClient) GetCaptcha(
	ctx context.Context,
) (
	*dto.CaptchaResponse,
	error,
) {
	flowClient, err := c.httpClient.WithCookieJar()
	if err != nil {
		return nil, err
	}

	// Trình duyệt luôn tải trang chủ trước khi gọi captcha. Request này cấp
	// cookie WAF nền (khác với các cookie được cấp bởi /api/captcha), nên mọi
	// bước xác thực sau đó phải dùng cùng cookie jar.
	if _, err := flowClient.GetBytes(
		ctx,
		c.endpoint+"/",
		c.buildDocumentHeaders(),
	); err != nil {
		return nil, err
	}

	var upstream dto.UpstreamCaptchaResponse

	if err := flowClient.GetJSON(
		ctx,
		c.endpoint+"/api/captcha",
		c.buildAcceptHeaders(),
		&upstream,
	); err != nil {
		return nil, err
	}

	targetURL, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, err
	}
	if len(
		session.SnapshotCookies(
			flowClient.Jar(),
			targetURL,
		),
	) == 0 {
		return nil, errors.New("captcha response did not establish cookies")
	}

	flowID, err := c.authFlows.create(flowClient)
	if err != nil {
		return nil, err
	}

	return &dto.CaptchaResponse{
		Key:     upstream.Key,
		Content: upstream.Content,
		FlowID:  flowID,
	}, nil
}

func (c *hddtgdtClient) Authenticate(
	ctx context.Context,
	req *dto.AuthenticationRequest,
) (
	*AuthenticationResult,
	error,
) {
	if req == nil {
		return nil, errors.New("authentication request is required")
	}

	flow, err := c.authFlows.consume(req.FlowID)
	if err != nil {
		return nil, err
	}

	upstreamRequest := &dto.UpstreamAuthenticationRequest{
		Username: req.Username,
		Password: req.Password,
		CValue:   req.CValue,
		CKey:     req.CKey,
	}

	var response dto.AuthenticationTokenResponse

	authHeaders := c.buildAcceptHeaders()
	authHeaders["Origin"] = c.endpoint
	if err := flow.client.PostJSON(
		ctx,
		c.endpoint+"/api/security-taxpayer/authenticate",
		authHeaders,
		upstreamRequest,
		&response,
	); err != nil {
		return nil, newAuthenticationStageError(
			AuthenticationStageAuthenticate,
			err,
		)
	}

	if response.Token == "" {
		return nil, newAuthenticationStageError(
			AuthenticationStageAuthenticate,
			errors.New("authenticate returned empty token"),
		)
	}

	targetURL, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, err
	}

	// GDT hiện yêu cầu JWT tồn tại trong cookie.
	flow.client.Jar().SetCookies(
		targetURL,
		[]*http.Cookie{
			{
				Name:   "jwt",
				Value:  response.Token,
				Path:   "/",
				Secure: targetURL.Scheme == "https",
			},
		},
	)

	// RẤT QUAN TRỌNG:
	// call profile sau authenticate để server cấp JSESSIONID
	// và refresh TS0114b13e
	if err := flow.client.GetJSON(
		ctx,
		c.endpoint+"/api/security-taxpayer/profile",
		c.buildAuthHeaders(response.Token),
		nil,
	); err != nil {
		return nil, newAuthenticationStageError(
			AuthenticationStageProfile,
			err,
		)
	}

	cookies := session.SnapshotCookies(
		flow.client.Jar(),
		targetURL,
	)

	if !hasCookie(
		cookies,
		"JSESSIONID",
	) {
		return nil, newAuthenticationStageError(
			AuthenticationStageProfile,
			errors.New(
				"profile succeeded but JSESSIONID cookie was not returned",
			),
		)
	}

	return &AuthenticationResult{
		Token:   response.Token,
		Cookies: cookies,
	}, nil
}

func hasCookie(
	cookies []session.Cookie,
	name string,
) bool {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value != "" {
			return true
		}
	}

	return false
}
