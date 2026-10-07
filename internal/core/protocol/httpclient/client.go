package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
)

// 8 * 2^10
const maxErrorBody = 8 << 10

type Client struct {
	client *http.Client
}

type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return e.Status
	}

	return fmt.Sprintf(
		"%s: %s",
		e.Status,
		e.Body,
	)
}

type BinaryResponse struct {
	Body               []byte
	ContentType        string
	ContentDisposition string
}

func New(timeout time.Duration) *Client {
	return &Client{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) WithCookieJar() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return c.WithExistingJar(jar), nil
}

func (c *Client) WithExistingJar(jar http.CookieJar) *Client {
	return &Client{
		client: &http.Client{
			Timeout:       c.client.Timeout,
			Transport:     c.client.Transport,
			CheckRedirect: c.client.CheckRedirect,
			Jar:           jar,
		},
	}
}

func (c *Client) Jar() http.CookieJar {
	if c == nil || c.client == nil {
		return nil
	}

	return c.client.Jar
}

func (c *Client) GetJSON(
	ctx context.Context,
	requestURL string,
	headers map[string]string,
	out any,
) error {
	return c.doJSON(
		ctx,
		http.MethodGet,
		requestURL,
		headers,
		nil,
		out,
	)
}

func (c *Client) PostJSON(
	ctx context.Context,
	requestURL string,
	headers map[string]string,
	body any,
	out any,
) error {
	return c.doJSON(
		ctx,
		http.MethodPost,
		requestURL,
		headers,
		body,
		out,
	)
}

func (c *Client) GetBytes(
	ctx context.Context,
	requestURL string,
	headers map[string]string,
) (
	*BinaryResponse,
	error,
) {
	return c.doBytes(
		ctx,
		http.MethodGet,
		requestURL,
		headers,
		nil,
	)
}

func (c *Client) doBytes(
	ctx context.Context,
	method string,
	requestURL string,
	headers map[string]string,
	body io.Reader,
) (
	*BinaryResponse,
	error,
) {
	req, err := http.NewRequestWithContext(
		ctx,
		method,
		requestURL,
		body,
	)
	if err != nil {
		return nil, err
	}

	applyHeaders(
		req,
		headers,
	)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	// 200-299
	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {
		return nil, readHTTPError(resp)
	}

	// TODO
	// Nếu file lớn có thể cân nhắc streaming
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &BinaryResponse{
		Body:               data,
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
	}, nil
}

func (c *Client) doJSON(
	ctx context.Context,
	method,
	requestURL string,
	headers map[string]string,
	body any,
	out any,
) error {
	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		requestURL,
		reader,
	)
	if err != nil {
		return err
	}

	applyHeaders(
		req,
		headers,
	)

	if body != nil {
		req.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	// 200-299
	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {
		return readHTTPError(resp)
	}

	if out == nil {
		_, err = io.Copy(
			io.Discard,
			resp.Body,
		)
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf(
			"decode response: %w",
			err,
		)
	}

	return nil
}

func applyHeaders(
	req *http.Request,
	headers map[string]string,
) {
	req.Header.Set(
		"User-Agent",
		"eif/1.0",
	)
	for key, value := range headers {
		req.Header.Set(
			key,
			value,
		)
	}
}

func readHTTPError(resp *http.Response) error {
	data, _ := io.ReadAll(
		io.LimitReader(
			resp.Body,
			maxErrorBody,
		),
	)
	return &HTTPError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       strings.TrimSpace(string(data)),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if retryAt, err := http.ParseTime(value); err == nil {
		if delay := time.Until(retryAt); delay > 0 {
			return delay
		}
	}

	return 0
}
