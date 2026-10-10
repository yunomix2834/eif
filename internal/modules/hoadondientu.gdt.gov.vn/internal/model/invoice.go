package model

import (
	"encoding/json"
	"time"
)

const MaxInvoiceQuerySize = 200

type InvoiceChannel string

const (
	InvoiceChannelStandard InvoiceChannel = "standard"
	InvoiceChannelSCO      InvoiceChannel = "sco"
)

type InvoiceDirection string

const (
	InvoiceDirectionSold     InvoiceDirection = "sold"
	InvoiceDirectionPurchase InvoiceDirection = "purchase"
)

type ExportFormat string

const (
	ExportFormatXLSX ExportFormat = "xlsx"
	ExportFormatCSV  ExportFormat = "csv"
	ExportFormatXML  ExportFormat = "xml"
	ExportFormatJSON ExportFormat = "json"
)

type InvoiceFilter struct {
	SoHoaDon         *int64
	KyHieuHoaDon     string
	KyHieuMauSo      *int
	MaSoThueNguoiBan string
	MaSoThueNguoiMua string
	TrangThaiHoaDon  *int
	KetQuaXuLy       *int
	HoaDonUyNhiem    *int
	CanCuocCongDan   string
}

type QueryOptions struct {
	From   time.Time
	To     time.Time
	Size   int
	Filter InvoiceFilter
}

type ExportOptions struct {
	From   time.Time
	To     time.Time
	Filter InvoiceFilter
}

type InvoiceQueryResult struct {
	FromDate string            `json:"from_date"`
	ToDate   string            `json:"to_date"`
	Records  []json.RawMessage `json:"datas"`
	Total    int               `json:"total"`
}

type File struct {
	Body        []byte
	ContentType string
	Filename    string
}
