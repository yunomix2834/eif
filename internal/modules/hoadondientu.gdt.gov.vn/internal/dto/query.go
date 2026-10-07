package dto

type InvoiceFilter struct {
	SoHoaDon         *int64 `json:"shdon,omitempty"`
	KyHieuHoaDon     string `json:"khhdon,omitempty"`
	KyHieuMauSo      *int   `json:"khmshdon,omitempty"`
	MaSoThueNguoiBan string `json:"nbmst,omitempty"`
	MaSoThueNguoiMua string `json:"nmmst,omitempty"`
	TrangThaiHoaDon  *int   `json:"tthai,omitempty"`
	KetQuaXuLy       *int   `json:"ttxly,omitempty"`
	HoaDonUyNhiem    *int   `json:"unhiem,omitempty"`
	CanCuocCongDan   string `json:"nmcmnd,omitempty"`
}

type HoaDonQuery struct {
	InvoiceFilter
	FromDate string `json:"from_date"`
	ToDate   string `json:"to_date"`
}
