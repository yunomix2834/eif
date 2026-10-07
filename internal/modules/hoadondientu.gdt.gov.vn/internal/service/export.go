package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yunotools/eif/internal/core/apperr"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/client"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	moduleutils "github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/utils"
	modulexlsx "github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/xlsx"
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
		file, err := s.exportInvoicesUpstream(
			ctx,
			auth,
			channel,
			direction,
			model.ExportOptions{
				From:   dateRange.From,
				To:     dateRange.To,
				Filter: prepared.filter,
			},
		)
		if err != nil {
			return nil, s.mapToAppError(err)
		}

		if file == nil || len(file.Body) == 0 {
			return nil, apperr.New(
				apperr.CodeHDDTGDTInvalidResponse,
				fmt.Errorf(
					"HDDT GDT returned an empty export workbook for %s to %s",
					moduleutils.FormatInputDate(dateRange.From),
					moduleutils.FormatInputDate(dateRange.To),
				),
			)
		}
		if err := modulexlsx.Validate(file.Body); err != nil {
			return nil, apperr.New(
				apperr.CodeHDDTGDTInvalidResponse,
				fmt.Errorf(
					"invalid export workbook for %s %s to %s: %w",
					channel,
					moduleutils.FormatInputDate(dateRange.From),
					moduleutils.FormatInputDate(dateRange.To),
					err,
				),
			)
		}
		files = append(
			files,
			file.Body,
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
