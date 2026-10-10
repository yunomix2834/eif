package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/yunomix2834/eif/internal/core/apperr"
	corehttp "github.com/yunomix2834/eif/internal/core/protocol/httpclient"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	moduleutils "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/utils"
	modulexlsx "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/xlsx"
)

type preparedExport struct {
	fromDate string
	toDate   string
	ranges   []moduleutils.DateRange
	filter   model.InvoiceFilter
}

func (s *service) ExportInvoiceSold(
	ctx context.Context,
	sessionID string,
	req *dto.ExportInvoiceRequest,
) (
	*model.File,
	error,
) {
	return s.exportInvoicesAll(
		ctx,
		sessionID,
		model.InvoiceDirectionSold,
		req,
	)
}

func (s *service) ExportInvoicePurchase(
	ctx context.Context,
	sessionID string,
	req *dto.ExportInvoiceRequest,
) (
	*model.File,
	error,
) {
	return s.exportInvoicesAll(
		ctx,
		sessionID,
		model.InvoiceDirectionPurchase,
		req,
	)
}

func (s *service) exportInvoicesAll(
	ctx context.Context,
	sessionID string,
	direction model.InvoiceDirection,
	req *dto.ExportInvoiceRequest,
) (
	result *model.File,
	resultErr error,
) {
	if req == nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			nil,
		)
	}

	prepared, err := s.prepareExport(
		req.InvoiceFilter,
		req.FromDate,
		req.ToDate,
	)
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
				"persist HDDT GDT cookies after invoice export failed",
				"session_id", sessionID,
				"error", syncErr,
			)
		}
	}()

	files := make(
		[][]byte,
		0,
		2,
	)
	mergeSources := make(
		[]modulexlsx.MergeSource,
		0,
		2,
	)
	for _, channel := range []model.InvoiceChannel{
		model.InvoiceChannelStandard,
		model.InvoiceChannelSCO,
	} {
		file, exportErr := s.exportChannelChunks(
			ctx,
			&auth,
			channel,
			direction,
			prepared,
		)
		if exportErr != nil {
			slog.Error(
				"HDDT GDT export failed",
				"channel",
				channel,
				"direction",
				direction,
				"from",
				req.FromDate,
				"to",
				req.ToDate,
				"error",
				exportErr,
			)
			return nil, exportErr
		}

		files = append(
			files,
			file.Body,
		)
		mergeSources = append(
			mergeSources,
			modulexlsx.MergeSource{
				Name: getExportSourceName(channel),
				Body: file.Body,
			},
		)
	}

	body, strictMergeErr := modulexlsx.Merge(files)
	if strictMergeErr != nil {
		slog.Info(
			"HDDT GDT standard/SCO schemas differ; merging by header",
			"direction",
			direction,
			"error",
			strictMergeErr,
		)
		body, err = modulexlsx.MergeByHeader(
			mergeSources,
			getExportTitle(direction),
		)
		if err != nil {
			return nil, apperr.New(
				apperr.CodeHDDTGDTInvalidResponse,
				fmt.Errorf(
					"merge standard and SCO export files: strict merge: %v; header merge: %w",
					strictMergeErr,
					err,
				),
			)
		}
	}

	// Tất cả format đều dùng chung workbook đã merge ở trên.
	// buildExportFile() chỉ chịu trách nhiệm bước cuối:
	//
	//	xlsx -> áp data type + number format kế toán
	//	csv  -> bảng UTF-8 cho Excel/phần mềm kế toán
	//	xml  -> dữ liệu máy đọc, giữ nguyên decimal text
	//	json -> dữ liệu tích hợp API/hệ thống khác
	//
	// -> query/chunk/merge chỉ có một luồng duy nhất, không tạo thêm
	// nhánh export riêng cho từng định dạng.
	return s.buildExportFile(
		body,
		direction,
		req.FromDate,
		req.ToDate,
		req.Format,
	)
}

func (s *service) prepareExport(
	invoiceFilter dto.InvoiceFilter,
	fromDate string,
	toDate string,
) (
	*preparedExport,
	error,
) {
	from, to, err := moduleutils.ParseDateRange(
		fromDate,
		toDate,
	)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			err,
		)
	}

	filter := mapToFilter(invoiceFilter)
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

	return &preparedExport{
		fromDate: fromDate,
		toDate:   toDate,
		ranges: moduleutils.SplitDateRangeDescending(
			from,
			to,
			s.maxExportDays,
		),
		filter: filter,
	}, nil
}

func (s *service) exportChannelChunks(
	ctx context.Context,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	prepared *preparedExport,
) (
	*model.File,
	error,
) {
	if prepared == nil {
		return nil, apperr.New(
			apperr.CodeInvalidRequest,
			nil,
		)
	}

	files := make(
		[][]byte,
		0,
		len(prepared.ranges),
	)
	for _, dateRange := range prepared.ranges {
		chunkFiles, err := s.exportDateRangeChunks(
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

		files = append(
			files,
			chunkFiles...,
		)
	}

	body, err := modulexlsx.Merge(files)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeHDDTGDTInvalidResponse,
			fmt.Errorf(
				"merge HDDT GDT export chunks: %w",
				err,
			),
		)
	}

	return &model.File{
		Body:        body,
		ContentType: modulexlsx.GetContentType(),
		Filename: fmt.Sprintf(
			"hddtgdt-%s-%s-%s_%s.xlsx",
			channel,
			direction,
			prepared.fromDate,
			prepared.toDate,
		),
	}, nil
}

// exportDateRangeChunks tải một khoảng ngày. Nếu HDDT GDT không phản hồi kịp,
// khoảng đó được chia đôi và thử lại theo thứ tự mới nhất -> cũ nhất.
//
// Không tăng timeout một cách mù quáng: endpoint export của GDT có những khoảng
// dữ liệu lớn không trả header dù chờ đủ 60 giây, trong khi cùng dữ liệu được
// chia nhỏ lại hoàn thành trong vài giây. Một ngày là giới hạn cuối cùng để
// recursion luôn dừng và lỗi timeout thật vẫn được trả về cho người dùng.
func (s *service) exportDateRangeChunks(
	ctx context.Context,
	auth *client.AuthenticatedContext,
	channel model.InvoiceChannel,
	direction model.InvoiceDirection,
	dateRange moduleutils.DateRange,
	filter model.InvoiceFilter,
) (
	[][]byte,
	error,
) {
	file, err := s.exportInvoicesUpstream(
		ctx,
		auth,
		channel,
		direction,
		model.ExportOptions{
			From:   dateRange.From,
			To:     dateRange.To,
			Filter: filter,
		},
	)
	if err != nil {
		newer, older, canSplit := splitExportDateRange(dateRange)
		if canSplit && isRetryableExportTimeout(ctx, err) {
			slog.Warn(
				"HDDT GDT export timed out; retrying smaller date ranges",
				"channel", channel,
				"direction", direction,
				"from", moduleutils.FormatInputDate(dateRange.From),
				"to", moduleutils.FormatInputDate(dateRange.To),
			)

			result := make([][]byte, 0, 2)
			for _, child := range []moduleutils.DateRange{newer, older} {
				childFiles, childErr := s.exportDateRangeChunks(
					ctx,
					auth,
					channel,
					direction,
					child,
					filter,
				)
				if childErr != nil {
					return nil, childErr
				}
				result = append(result, childFiles...)
			}
			return result, nil
		}
		return nil, err
	}

	if file == nil || len(file.Body) == 0 {
		return nil, fmt.Errorf(
			"HDDT GDT returned an empty export workbook for %s to %s",
			moduleutils.FormatInputDate(dateRange.From),
			moduleutils.FormatInputDate(dateRange.To),
		)
	}
	if err := modulexlsx.Validate(file.Body); err != nil {
		return nil, fmt.Errorf(
			"invalid export workbook for %s %s to %s: %w",
			channel,
			moduleutils.FormatInputDate(dateRange.From),
			moduleutils.FormatInputDate(dateRange.To),
			err,
		)
	}

	return [][]byte{file.Body}, nil
}

func isRetryableExportTimeout(
	ctx context.Context,
	err error,
) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var httpErr *corehttp.HTTPError
	return errors.As(err, &httpErr) &&
		httpErr.StatusCode == http.StatusGatewayTimeout
}

func splitExportDateRange(
	value moduleutils.DateRange,
) (
	newer moduleutils.DateRange,
	older moduleutils.DateRange,
	ok bool,
) {
	days := moduleutils.CalculateInclusiveDays(value.From, value.To)
	if days <= 1 {
		return moduleutils.DateRange{}, moduleutils.DateRange{}, false
	}

	splitAt := time.Date(
		value.From.Year(),
		value.From.Month(),
		value.From.Day(),
		0,
		0,
		0,
		0,
		value.From.Location(),
	).AddDate(0, 0, days/2)

	return moduleutils.DateRange{
			From: splitAt,
			To:   value.To,
		}, moduleutils.DateRange{
			From: value.From,
			To:   splitAt.Add(-time.Second),
		}, true
}

func getExportSourceName(channel model.InvoiceChannel) string {
	switch channel {
	case model.InvoiceChannelStandard:
		return "Hóa đơn thường"
	case model.InvoiceChannelSCO:
		return "Máy tính tiền"
	default:
		return string(channel)
	}
}

func getExportTitle(direction model.InvoiceDirection) string {
	if direction == model.InvoiceDirectionPurchase {
		return "Hóa đơn mua vào"
	}
	return "Hóa đơn bán ra"
}
