package client

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	corehttp "github.com/yunotools/eif/internal/core/protocol/httpclient"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/session"
)

type hddtgdtClient struct {
	httpClient *corehttp.Client
	endpoint   string
	authFlows  *authFlowStore
}

func New(
	httpClient *corehttp.Client,
	endpoint string,
) Client {
	return &hddtgdtClient{
		httpClient: httpClient,
		endpoint:   endpoint,
		authFlows:  newAuthFlowStore(5 * time.Minute),
	}
}

func (c *hddtgdtClient) buildDocumentHeaders() map[string]string {
	return map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "vi",
	}
}

func (c *hddtgdtClient) buildAcceptHeaders() map[string]string {
	headers := map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "vi",
		"Action":          "",
		"End-Point":       "/",
		"Referer": strings.TrimRight(
			c.endpoint,
			"/",
		) + "/",
	}

	if requestID, err := newUpstreamRequestID(); err == nil {
		headers["request-id"] = requestID
	}

	return headers
}

func (c *hddtgdtClient) buildAuthHeaders(token string) map[string]string {
	headers := c.buildAcceptHeaders()
	headers["Authorization"] = "Bearer " + token
	return headers
}

func newUpstreamRequestID() (
	string,
	error,
) {
	buffer := make(
		[]byte,
		16,
	)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = buffer[6]&0x0f | 0x40
	buffer[8] = buffer[8]&0x3f | 0x80

	value := hex.EncodeToString(buffer)
	return value[:8] + "-" +
		value[8:12] + "-" +
		value[12:16] + "-" +
		value[16:20] + "-" +
		value[20:], nil
}

func (c *hddtgdtClient) authenticatedClient(
	auth *AuthenticatedContext,
) (
	*corehttp.Client,
	*url.URL,
	error,
) {
	if auth == nil {
		return nil, nil, fmt.Errorf("authentication context is required")
	}
	if auth.Token == "" {
		return nil, nil, fmt.Errorf("authentication token is required")
	}
	if len(auth.Cookies) == 0 {
		return nil, nil, fmt.Errorf("authentication cookies are required")
	}

	httpClient, err := c.httpClient.WithCookieJar()
	if err != nil {
		return nil, nil, err
	}

	target, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, nil, err
	}

	session.RestoreCookies(
		httpClient.Jar(),
		target,
		auth.Cookies,
	)

	// JWT luôn đồng bộ với Token
	httpClient.Jar().SetCookies(
		target,
		[]*http.Cookie{
			{
				Name:   "jwt",
				Value:  auth.Token,
				Path:   "/",
				Secure: target.Scheme == "https",
			},
		},
	)

	return httpClient, target, nil
}

func updateAuthenticatedContext(
	httpClient *corehttp.Client,
	target *url.URL,
	auth *AuthenticatedContext,
) {
	if httpClient == nil || target == nil || auth == nil {
		return
	}

	cookies := session.SnapshotCookies(
		httpClient.Jar(),
		target,
	)
	if len(cookies) > 0 {
		auth.Cookies = cookies
	}
}
