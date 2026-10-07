package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/yunotools/eif/internal/core/apperr"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	moduleutils "github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/utils"
)

func (s *service) QueryInvoiceSold(
	ctx context.Context,
	sessionID string,
	query *dto.HoaDonQuery,
) (
	*model.InvoiceQueryResult,
	error,
) {
	return s.queryInvoicesAll(
		ctx,
		sessionID,
		model.InvoiceDirectionSold,
		query,
	)
}

func (s *service) QueryInvoicePurchase(
	ctx context.Context,
	sessionID string,
	query *dto.HoaDonQuery,
) (
	*model.InvoiceQueryResult,
	error,
) {
	return s.queryInvoicesAll(
		ctx,
		sessionID,
		model.InvoiceDirectionPurchase,
		query,
	)
}

type preparedInvoiceQuery struct {
	fromDate string
	toDate   string
	from     time.Time
	to       time.Time
	ranges   []moduleutils.DateRange
	filter   model.InvoiceFilter
}

type allRangeResult struct {
	records []json.RawMessage
}

func (s *service) queryInvoicesAll(
	ctx context.Context,
	sessionID string,
	direction model.InvoiceDirection,
	query *dto.HoaDonQuery,
) (
	result *model.InvoiceQueryResult,
	resultErr error,
) {
	prepared, err := s.prepareInvoiceQuery(query)
	if err != nil {
		return nil, err
	}

	auth, err := s.getAuthContext(sessionID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if syncErr := s.syncAuthCookies(sessionID, &auth); syncErr != nil {
			if resultErr == nil {
				result = nil
				resultErr = syncErr
				return
			}
			slog.Error(
				"persist HDDT GDT cookies after invoice query failed",
				"session_id", sessionID,
				"error", syncErr,
			)
		}
	}()

	merged := &model.InvoiceQueryResult{
		FromDate: prepared.fromDate,
		ToDate:   prepared.toDate,
		Records: make(
			[]json.RawMessage,
			0,
		),
	}

	for _, channel := range []model.InvoiceChannel{
		model.InvoiceChannelStandard,
		model.InvoiceChannelSCO,
	} {
		channelResult, queryErr := s.queryInvoicesWithAuth(
			ctx,
			sessionID,
			&auth,
			channel,
			direction,
			prepared,
		)
		if queryErr != nil {
			slog.Error(
				"HDDT GDT invoice source query failed",
				"channel",
				channel,
				"direction",
				direction,
				"from",
				prepared.fromDate,
				"to",
				prepared.toDate,
				"error",
				queryErr,
			)
			return nil, queryErr
		}

		merged.Records = append(
			merged.Records,
			channelResult.Records...,
		)
	}

	sortInvoiceRecordsByCreatedAt(merged.Records)
	merged.Total = len(merged.Records)
	return merged, nil
}

func (s *service) prepareInvoiceQuery(query *dto.HoaDonQuery) (
	*preparedInvoiceQuery,
	error,
) {
	if query == nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			nil,
		)
	}

	from, to, err := moduleutils.ParseDateRange(
		query.FromDate,
		query.ToDate,
	)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			err,
		)
	}

	filter := mapToFilter(query.InvoiceFilter)
	if _, err := moduleutils.BuildSearch(
		from,
		to,
		filter,
	); err != nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			err,
		)
	}

	return &preparedInvoiceQuery{
		fromDate: query.FromDate,
		toDate:   query.ToDate,
		from:     from,
		to:       to,
		ranges: moduleutils.SplitDateRangeDescending(
			from,
			to,
			s.maxQueryDays,
		),
		filter: filter,
	}, nil
}

// queryInvoicesWithAuth tải toàn bộ dataset của một channel và
// cache theo EIF session + channel + direction + normalized search.
// Page không còn nằm trong API/backend cache:
// browser tự chia trang sau khi nhận dataset.
func (s *service) queryInvoicesWithAuth(
	ctx context.Context,
	sessionID string,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	prepared *preparedInvoiceQuery,
) (
	*model.InvoiceQueryResult,
	error,
) {
	if prepared == nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			nil,
		)
	}

	cacheKey := getInvoiceDatasetCacheKey(
		sessionID,
		channel,
		direction,
		prepared,
	)
	if cached, ok := s.queryCache.get(cacheKey); ok {
		return cached, nil
	}

	// EIF là desktop app và HDDTGDT có rate-limit chặt.
	// Chỉ cho một full-query collector chạy tại một thời điểm;
	// request giống nhau chờ phía sau sẽ hit cache.
	s.queryMu.Lock()
	defer s.queryMu.Unlock()

	if cached, ok := s.queryCache.get(cacheKey); ok {
		return cached, nil
	}

	merged := &model.InvoiceQueryResult{
		FromDate: prepared.fromDate,
		ToDate:   prepared.toDate,
		Records: make(
			[]json.RawMessage,
			0,
		),
	}

	for _, dateRange := range prepared.ranges {
		rangeResult, err := s.queryAllRange(
			ctx,
			auth,
			channel,
			direction,
			dateRange,
			prepared.filter,
		)
		if err != nil {
			return nil, s.mapToAppError(err)
		}

		merged.Records = append(
			merged.Records,
			rangeResult.records...,
		)
	}

	sortInvoiceRecordsByCreatedAt(merged.Records)
	merged.Total = len(merged.Records)

	s.queryCache.set(
		cacheKey,
		merged,
	)

	return merged, nil
}

// queryAllRange dùng total của HDDTGDT để chia đôi khoảng thời gian cho tới khi
// mỗi request có <= 50 bản ghi.
// Nhờ vậy một lần tra cứu EIF lấy toàn bộ dữ liệu dù
// endpoint HDDTGDT chỉ trả tối đa 50 bản ghi mỗi request.
func (s *service) queryAllRange(
	ctx context.Context,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	dateRange moduleutils.DateRange,
	filter model.InvoiceFilter,
) (
	*allRangeResult,
	error,
) {
	response, err := s.queryInvoicesUpstream(
		ctx,
		auth,
		channel,
		direction,
		model.QueryOptions{
			From:   dateRange.From,
			To:     dateRange.To,
			Size:   model.MaxInvoiceQuerySize,
			Filter: filter,
		},
	)
	if err != nil {
		return nil, err
	}

	if response == nil {
		return nil, fmt.Errorf("HDDT GDT returned an empty invoice query response")
	}

	result := &allRangeResult{}

	if response.Total <= len(response.Records) {
		result.records = append(
			result.records,
			response.Records...,
		)
		return result, nil
	}

	if response.Total <= model.MaxInvoiceQuerySize {
		return nil, fmt.Errorf(
			"HDDT GDT reported %d invoices but returned only %d",
			response.Total,
			len(response.Records),
		)
	}

	newer, older, ok := splitAdaptiveRange(dateRange)
	if !ok {
		return nil, fmt.Errorf(
			"cannot load all %d invoices between %s and %s because the GDT query endpoint returns at most %d records for the same second",
			response.Total,
			moduleutils.FormatInputDate(dateRange.From),
			moduleutils.FormatInputDate(dateRange.To),
			model.MaxInvoiceQuerySize,
		)
	}

	newerResult, err := s.queryAllRange(
		ctx,
		auth,
		channel,
		direction,
		newer,
		filter,
	)
	if err != nil {
		return nil, err
	}
	olderResult, err := s.queryAllRange(
		ctx,
		auth,
		channel,
		direction,
		older,
		filter,
	)
	if err != nil {
		return nil, err
	}

	result.records = append(
		result.records,
		newerResult.records...,
	)
	result.records = append(
		result.records,
		olderResult.records...,
	)

	return result, nil
}

func getInvoiceDatasetCacheKey(
	sessionID string,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	prepared *preparedInvoiceQuery,
) string {
	sessionHash := sha256.Sum256([]byte(sessionID))
	search, _ := moduleutils.BuildSearch(
		prepared.from,
		prepared.to,
		prepared.filter,
	)
	return fmt.Sprintf(
		"%x|%s|%s|%s",
		sessionHash,
		channel,
		direction,
		search,
	)
}

func splitAdaptiveRange(
	value moduleutils.DateRange,
) (
	newer moduleutils.DateRange,
	older moduleutils.DateRange,
	ok bool,
) {
	from := value.From.Truncate(time.Second)
	to := value.To.Truncate(time.Second)
	if !to.After(from) {
		return moduleutils.DateRange{}, moduleutils.DateRange{}, false
	}

	seconds := int64(to.Sub(from) / time.Second)
	midpoint := from.Add(time.Duration(seconds/2) * time.Second)
	newerFrom := midpoint.Add(time.Second)
	if newerFrom.After(to) {
		return moduleutils.DateRange{}, moduleutils.DateRange{}, false
	}

	return moduleutils.DateRange{
			From: newerFrom,
			To:   to,
		}, moduleutils.DateRange{
			From: from,
			To:   midpoint,
		}, true
}

func sortInvoiceRecordsByCreatedAt(records []json.RawMessage) {
	sort.SliceStable(
		records,
		func(i, j int) bool {
			left, leftOK := getInvoiceCreatedAt(records[i])
			right, rightOK := getInvoiceCreatedAt(records[j])
			if leftOK != rightOK {
				return leftOK
			}
			if !leftOK {
				return false
			}
			return left.After(right)
		},
	)
}

func getInvoiceCreatedAt(
	raw json.RawMessage,
) (
	time.Time,
	bool,
) {
	var value struct {
		CreatedAt string `json:"tdlap"`
	}

	if err := json.Unmarshal(
		raw,
		&value,
	); err != nil {
		return time.Time{}, false
	}

	input := strings.TrimSpace(value.CreatedAt)
	if input == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"02/01/2006T15:04:05",
		"02/01/2006 15:04:05",
		"02/01/2006",
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(
			layout,
			input,
		); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}
