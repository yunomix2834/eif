package service

import (
	"errors"
	"strings"

	"github.com/yunotools/eif/internal/core/apperr"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/dto"
	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
)

func mapToFilter(filter dto.InvoiceFilter) model.InvoiceFilter {
	return model.InvoiceFilter{
		SoHoaDon:         filter.SoHoaDon,
		KyHieuHoaDon:     strings.TrimSpace(filter.KyHieuHoaDon),
		KyHieuMauSo:      filter.KyHieuMauSo,
		MaSoThueNguoiBan: strings.TrimSpace(filter.MaSoThueNguoiBan),
		MaSoThueNguoiMua: strings.TrimSpace(filter.MaSoThueNguoiMua),
		TrangThaiHoaDon:  filter.TrangThaiHoaDon,
		KetQuaXuLy:       filter.KetQuaXuLy,
		HoaDonUyNhiem:    filter.HoaDonUyNhiem,
		CanCuocCongDan:   strings.TrimSpace(filter.CanCuocCongDan),
	}
}

func getDirectionFromString(value string) (
	model.InvoiceDirection,
	error,
) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(model.InvoiceDirectionSold):
		return model.InvoiceDirectionSold, nil

	case string(model.InvoiceDirectionPurchase):
		return model.InvoiceDirectionPurchase, nil

	default:
		return "", apperr.New(
			apperr.CodeInvalidRequest,
			errors.New("type must be sold or purchase"),
		)
	}
}
