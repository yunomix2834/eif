package dto

type ExportInvoiceRequest struct {
	InvoiceFilter
	FromDate string `json:"from_date"`
	ToDate   string `json:"to_date"`
	Format   string `json:"format,omitempty"`
}
