package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yunotools/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
)

var (
	maThuePattern       = regexp.MustCompile(`^[0-9-]{1,32}$`)
	kyHieuHoaDonPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,64}$`)
	cccdPattern         = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
)

func BuildSearch(
	from,
	to time.Time,
	filter model.InvoiceFilter,
) (
	string,
	error,
) {
	parts := []string{
		"tdlap=ge=" + FormatHDDTGDTDate(from),
		"tdlap=le=" + FormatHDDTGDTDate(to),
	}

	if filter.SoHoaDon != nil {
		if *filter.SoHoaDon < 0 {
			return "", fmt.Errorf("shdon must be >= 0")
		}
		parts = append(
			parts,
			"shdon=="+strconv.FormatInt(
				*filter.SoHoaDon,
				10,
			),
		)
	}

	if filter.KyHieuHoaDon != "" {
		if !kyHieuHoaDonPattern.MatchString(filter.KyHieuHoaDon) {
			return "", fmt.Errorf("khhdon contains unsupported characters")
		}
		parts = append(
			parts,
			"khhdon=="+filter.KyHieuHoaDon,
		)
	}

	if filter.KyHieuMauSo != nil {
		if *filter.KyHieuMauSo < 0 {
			return "", fmt.Errorf("khmshdon must be >= 0")
		}
		parts = append(
			parts,
			"khmshdon=="+strconv.Itoa(*filter.KyHieuMauSo),
		)
	}

	if filter.MaSoThueNguoiBan != "" {
		if !maThuePattern.MatchString(filter.MaSoThueNguoiBan) {
			return "", fmt.Errorf("nbmst is invalid")
		}
		parts = append(
			parts,
			"nbmst=="+filter.MaSoThueNguoiBan,
		)
	}

	if filter.MaSoThueNguoiMua != "" {
		if !maThuePattern.MatchString(filter.MaSoThueNguoiMua) {
			return "", fmt.Errorf("nmmst is invalid")
		}
		parts = append(
			parts,
			"nmmst=="+filter.MaSoThueNguoiMua,
		)
	}

	if filter.TrangThaiHoaDon != nil {
		if *filter.TrangThaiHoaDon < 1 || *filter.TrangThaiHoaDon > 6 {
			return "", fmt.Errorf("tthai must be between 1 and 6")
		}
		parts = append(
			parts,
			"tthai=="+strconv.Itoa(*filter.TrangThaiHoaDon),
		)
	}

	if filter.KetQuaXuLy != nil {
		if *filter.KetQuaXuLy < 0 || *filter.KetQuaXuLy > 8 {
			return "", fmt.Errorf("ttxly must be between 0 and 8")
		}
		parts = append(
			parts,
			"ttxly=="+strconv.Itoa(*filter.KetQuaXuLy),
		)
	}

	if filter.HoaDonUyNhiem != nil {
		if *filter.HoaDonUyNhiem != 0 && *filter.HoaDonUyNhiem != 1 {
			return "", fmt.Errorf("unhiem must be 0 or 1")
		}
		parts = append(
			parts,
			"unhiem=="+strconv.Itoa(*filter.HoaDonUyNhiem),
		)
	}

	if filter.CanCuocCongDan != "" {
		if !cccdPattern.MatchString(filter.CanCuocCongDan) {
			return "", fmt.Errorf("nmcmnd is invalid")
		}
		parts = append(
			parts,
			"nmcmnd=="+filter.CanCuocCongDan,
		)
	}

	return strings.Join(
		parts,
		";",
	), nil
}
