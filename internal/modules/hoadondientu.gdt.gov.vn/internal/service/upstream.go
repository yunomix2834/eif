package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	corehttp "github.com/yunotools/eif/internal/core/protocol/httpclient"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
)

const (
	maxRateLimitBackoff  = 15 * time.Second // Giới hạn thời gian chờ retry tối đa
	queryCacheMaxEntries = 32               // Cache tối đa 32 query khác nhau
	queryCacheMaxBytes   = 256 << 20        // Cache không được vượt quá 256MB RAM
)

// semaphore: bộ đếm giới hạn
// là một cơ chế đồng bộ trong lập trình dùng để
// giới hạn số lượng goroutine/thread được phép chạy cùng lúc vào một vùng tài nguyên
type upstreamLimiter struct {
	slot        chan struct{} // Không cho quá nhiều request đồng thời
	mu          sync.Mutex    // Bảo vệ concurrent access
	nextStartAt time.Time     // Giãn khoảng cách request
	minInterval time.Duration
}

func newUpstreamLimiter(minInterval time.Duration) *upstreamLimiter {
	if minInterval < 0 {
		minInterval = 0
	}

	return &upstreamLimiter{
		slot: make(
			chan struct{},
			1,
		),
		minInterval: minInterval,
	}
}

// acquire xin quyền gọi HDDTGDT
func (l *upstreamLimiter) acquire(ctx context.Context) (
	func(),
	error,
) {
	select {
	// B1: lấy slot
	case l.slot <- struct{}{}:

	// Nếu đầy -> chờ tới khi:
	// timeout
	// hoặc
	// request bị cancel
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// tạo release function
	release := func() { <-l.slot }

	l.mu.Lock()
	wait := time.Until(l.nextStartAt)
	l.mu.Unlock()

	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}

	l.mu.Lock()
	l.nextStartAt = time.Now().Add(l.minInterval)
	l.mu.Unlock()

	return release, nil
}

func (l *upstreamLimiter) cooldown(delay time.Duration) {
	if l == nil || delay <= 0 {
		return
	}

	until := time.Now().Add(delay)

	l.mu.Lock()
	if until.After(l.nextStartAt) {
		l.nextStartAt = until
	}
	l.mu.Unlock()
}

type invoiceQueryCacheEntry struct {
	expiresAt time.Time
	bytes     int
	result    *model.InvoiceQueryResult
}

type invoiceQueryCache struct { // bộ nhớ cache.
	mu         sync.Mutex
	ttl        time.Duration
	totalBytes int
	items      map[string]invoiceQueryCacheEntry
}

func newInvoiceQueryCache(ttl time.Duration) *invoiceQueryCache {
	return &invoiceQueryCache{
		ttl:   ttl,
		items: make(map[string]invoiceQueryCacheEntry),
	}
}

// lấy cache
func (c *invoiceQueryCache) get(key string) (
	*model.InvoiceQueryResult,
	bool,
) {
	// cache disabled
	if c == nil || c.ttl <= 0 {
		return nil, false
	}

	// map Go không thread-safe.
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}

	if !entry.expiresAt.After(now) {
		c.removeLocked(key)
		return nil, false
	}

	// clone trước khi trả, không trả pointer gốc
	return cloneInvoiceQueryResult(entry.result), true
}

// lưu cache
func (c *invoiceQueryCache) set(
	key string,
	result *model.InvoiceQueryResult,
) {
	if c == nil || c.ttl <= 0 || result == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.removeLocked(key)

	cloned := cloneInvoiceQueryResult(result)
	entryBytes := calculateInvoiceQueryResultBytes(cloned) // biết RAM đang dùng bao nhiêu

	c.items[key] = invoiceQueryCacheEntry{
		expiresAt: now.Add(c.ttl),
		bytes:     entryBytes,
		result:    cloned,
	}

	c.totalBytes += entryBytes

	// dọn cache
	// xóa cache hết hạn
	// hoặc
	// quá lớn
	c.pruneLocked(now)
}

// dọn cache
func (c *invoiceQueryCache) pruneLocked(now time.Time) {
	for key, entry := range c.items {
		if !entry.expiresAt.After(now) {
			c.removeLocked(key)
		}
	}

	for len(c.items) > queryCacheMaxEntries || c.totalBytes > queryCacheMaxBytes {
		oldestKey := ""
		var oldestExpiry time.Time
		for key, entry := range c.items {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
		}
		if oldestKey == "" {
			break
		}
		c.removeLocked(oldestKey)
	}
}

// xoá cache
func (c *invoiceQueryCache) removeLocked(key string) {
	entry, ok := c.items[key]
	if !ok {
		return
	}
	delete(
		c.items,
		key,
	)

	c.totalBytes -= entry.bytes
	if c.totalBytes < 0 {
		c.totalBytes = 0
	}
}

func calculateInvoiceQueryResultBytes(result *model.InvoiceQueryResult) int {
	if result == nil {
		return 0
	}

	total := 0

	for _, raw := range result.Records {
		total += len(raw)
	}

	return total
}

func cloneInvoiceQueryResult(
	source *model.InvoiceQueryResult,
) *model.InvoiceQueryResult {
	if source == nil {
		return nil
	}

	cloned := *source
	cloned.Records = make(
		[]json.RawMessage,
		len(source.Records),
	)
	for index, raw := range source.Records {
		cloned.Records[index] = append(
			json.RawMessage(nil),
			raw...,
		)
	}

	return &cloned
}

func (s *service) queryInvoicesUpstream(
	ctx context.Context,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	opts model.QueryOptions,
) (
	*model.InvoiceQueryResult,
	error,
) {
	if opts.Size <= 0 ||
		opts.Size > model.MaxInvoiceQuerySize {
		opts.Size = model.MaxInvoiceQuerySize
	}

	var lastErr error
	for attempt := 0; attempt <= s.rateLimitRetries; attempt++ {
		release, err := s.upstreamLimiter.acquire(ctx)
		if err != nil {
			return nil, err
		}

		result, requestErr, delay := func() (
			*model.InvoiceQueryResult,
			error,
			time.Duration,
		) {
			defer release()
			result, requestErr := s.client.QueryInvoices(
				ctx,
				auth,
				channel,
				direction,
				opts,
			)
			if !isRateLimitError(requestErr) {
				return result, requestErr, 0
			}

			delay := s.rateLimitDelay(
				requestErr,
				attempt,
			)
			s.upstreamLimiter.cooldown(delay)

			return result, requestErr, delay
		}()

		if requestErr == nil {
			return result, nil
		}

		lastErr = requestErr
		if !isRateLimitError(requestErr) ||
			attempt >= s.rateLimitRetries {
			return nil, requestErr
		}

		slog.Warn(
			"HDDT GDT rate limited invoice query",
			"channel",
			channel,
			"direction",
			direction,
			"attempt",
			attempt+1,
			"retry_after",
			delay,
		)

		if err := waitContext(
			ctx,
			delay,
		); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

func (s *service) exportInvoicesUpstream(
	ctx context.Context,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	opts model.ExportOptions,
) (
	*model.File,
	error,
) {
	var lastErr error
	for attempt := 0; attempt <= s.rateLimitRetries; attempt++ {
		release, err := s.upstreamLimiter.acquire(ctx)
		if err != nil {
			return nil, err
		}

		result, requestErr, delay := func() (
			*model.File,
			error,
			time.Duration,
		) {
			defer release()
			result, requestErr := s.client.ExportInvoices(
				ctx,
				auth,
				channel,
				direction,
				opts,
			)
			if !isRateLimitError(requestErr) {
				return result, requestErr, 0
			}

			delay := s.rateLimitDelay(
				requestErr,
				attempt,
			)
			s.upstreamLimiter.cooldown(delay)
			return result, requestErr, delay
		}()

		if requestErr == nil {
			return result, nil
		}

		lastErr = requestErr
		if !isRateLimitError(requestErr) ||
			attempt >= s.rateLimitRetries {
			return nil, requestErr
		}

		slog.Warn(
			"HDDT GDT rate limited export",
			"channel",
			channel,
			"direction",
			direction,
			"attempt",
			attempt+1,
			"retry_after",
			delay,
		)

		if err := waitContext(
			ctx,
			delay,
		); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

func isRateLimitError(err error) bool {
	var httpErr *corehttp.HTTPError
	return errors.As(
		err,
		&httpErr,
	) &&
		httpErr.StatusCode == http.StatusTooManyRequests
}

// rateLimitDelay exponential backoff
// 0: 1s
// 1: 2s
// 2: 4s
// 3: 8s
// 4: 15s(max)
func (s *service) rateLimitDelay(
	err error,
	attempt int,
) time.Duration {
	delay := s.rateLimitBaseDelay
	if delay <= 0 {
		delay = time.Second
	}

	for index := 0; index < attempt && delay < maxRateLimitBackoff; index++ {
		delay *= 2
	}

	if delay > maxRateLimitBackoff {
		delay = maxRateLimitBackoff
	}

	var httpErr *corehttp.HTTPError
	if errors.As(
		err,
		&httpErr,
	) &&
		httpErr.RetryAfter > delay {
		delay = httpErr.RetryAfter
	}

	return delay
}

// waitContext chờ nhưng có thể cancel
// Không dùng: time.Sleep() vì không thể cancel
func waitContext(
	ctx context.Context,
	delay time.Duration,
) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)

	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
