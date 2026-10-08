package service

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/yunomix2834/eif/internal/core/apperr"
	"github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/model"
	modulexlsx "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn/internal/xlsx"
)

const accountingExportVersion = 1

// accountingExportColumn mô tả metadata của một cột trong XML/JSON.
//
// Type hiện có hai giá trị:
//
//	text    -> mã, ký hiệu, ngày hiển thị, tên, địa chỉ...
//	decimal -> các trường tiền
//
// CSV không có khái niệm metadata kiểu dữ liệu nên chỉ ghi header + raw value.
type accountingExportColumn struct {
	Name string `json:"name" xml:"name,attr"`
	Type string `json:"type" xml:"type,attr"`
}

// accountingExportPayload là cấu trúc trung gian cho JSON.
// Rows dùng [][]string thay vì []map[string]any vì:
//
//  1. không đi qua float64 -> không làm sai số tiền;
//  2. giữ đúng thứ tự cột của file Excel;
//  3. không bị mất dữ liệu nếu upstream có hai header trùng tên.
type accountingExportPayload struct {
	Version   int                      `json:"version"`
	Direction model.InvoiceDirection   `json:"direction"`
	FromDate  string                   `json:"from_date"`
	ToDate    string                   `json:"to_date"`
	Columns   []accountingExportColumn `json:"columns"`
	Rows      [][]string               `json:"rows"`
}

// XML dùng <cell> với attribute column/type thay vì biến trực tiếp tên header
// tiếng Việt thành XML tag. Cách này tạo schema ổn định, không phụ thuộc dấu,
// khoảng trắng hoặc ký tự '/' trong tên cột.
type accountingXMLExport struct {
	XMLName   xml.Name               `xml:"invoice_export"`
	Version   int                    `xml:"version,attr"`
	Direction model.InvoiceDirection `xml:"direction,attr"`
	FromDate  string                 `xml:"from_date,attr"`
	ToDate    string                 `xml:"to_date,attr"`
	Columns   accountingXMLColumns   `xml:"columns"`
	Rows      accountingXMLRows      `xml:"rows"`
}

type accountingXMLColumns struct {
	Items []accountingXMLColumn `xml:"column"`
}

type accountingXMLColumn struct {
	Index int    `xml:"index,attr"`
	Name  string `xml:"name,attr"`
	Type  string `xml:"type,attr"`
}

type accountingXMLRows struct {
	Items []accountingXMLRow `xml:"row"`
}

type accountingXMLRow struct {
	Index int                 `xml:"index,attr"`
	Cells []accountingXMLCell `xml:"cell"`
}

type accountingXMLCell struct {
	Column string `xml:"column,attr"`
	Type   string `xml:"type,attr"`
	Value  string `xml:",chardata"`
}

// buildExportFile là điểm duy nhất quyết định output format sau khi dữ liệu đã
// query theo chunk và merge xong. Vì vậy việc thêm CSV/XML/JSON không làm phát
// sinh thêm request lên HDDTGDT và không thay đổi giới hạn ngày hiện có.
func (s *service) buildExportFile(
	body []byte,
	direction model.InvoiceDirection,
	fromDate string,
	toDate string,
	formatValue string,
) (
	*model.File,
	error,
) {
	format, err := parseExportFormat(formatValue)
	if err != nil {
		return nil, err
	}

	baseFilename := fmt.Sprintf(
		"hddtgdt-%s-%s_%s",
		direction,
		fromDate,
		toDate,
	)

	if format == model.ExportFormatXLSX {
		// XLSX cần thêm một bước riêng để ép các cột định danh thành Text và
		// các cột tiền thành numeric + Custom Number Format.
		formattedBody, err := modulexlsx.FormatInvoiceExport(body)
		if err != nil {
			return nil, apperr.New(
				apperr.CodeHDDTGDTInvalidResponse,
				fmt.Errorf(
					"format merged export workbook: %w",
					err,
				),
			)
		}

		return &model.File{
			Body:        formattedBody,
			ContentType: modulexlsx.GetContentType(),
			Filename:    baseFilename + ".xlsx",
		}, nil
	}

	// CSV/XML/JSON không bị giới hạn kiểu cell của Excel. Tuy nhiên vẫn chạy
	// NormalizeNumericDisplay() trước khi đọc bảng để scientific notation được
	// mở rộng thành decimal text nếu workbook nguồn có dạng 1.23E+10.
	normalizedBody, err := modulexlsx.NormalizeNumericDisplay(body)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeHDDTGDTInvalidResponse,
			fmt.Errorf(
				"normalize merged export workbook: %w",
				err,
			),
		)
	}

	table, err := modulexlsx.ReadExportTable(normalizedBody)
	if err != nil {
		return nil, apperr.New(
			apperr.CodeHDDTGDTInvalidResponse,
			fmt.Errorf(
				"read merged export table: %w",
				err,
			),
		)
	}

	switch format {
	case model.ExportFormatCSV:
		data, err := encodeAccountingCSV(table)
		if err != nil {
			return nil, apperr.New(
				apperr.CodeInternalError,
				err,
			)
		}
		return &model.File{
			Body:        data,
			ContentType: "text/csv; charset=utf-8",
			Filename:    baseFilename + ".csv",
		}, nil

	case model.ExportFormatXML:
		data, err := encodeAccountingXML(
			table,
			direction,
			fromDate,
			toDate,
		)
		if err != nil {
			return nil, apperr.New(
				apperr.CodeInternalError,
				err,
			)
		}
		return &model.File{
			Body:        data,
			ContentType: "application/xml; charset=utf-8",
			Filename:    baseFilename + ".xml",
		}, nil

	case model.ExportFormatJSON:
		data, err := encodeAccountingJSON(
			table,
			direction,
			fromDate,
			toDate,
		)
		if err != nil {
			return nil, apperr.New(
				apperr.CodeInternalError,
				err,
			)
		}
		return &model.File{
			Body:        data,
			ContentType: "application/json; charset=utf-8",
			Filename:    baseFilename + ".json",
		}, nil
	}

	return nil, apperr.New(
		apperr.CodeInvalidRequest,
		errors.New("unsupported export format"),
	)
}

// parseExportFormat giữ backward compatibility: FE cũ không gửi format sẽ vẫn
// nhận XLSX như trước. Giá trị không hợp lệ bị trả về Invalid Request thay vì
// tự đoán format để tránh tải nhầm loại file.
func parseExportFormat(value string) (
	model.ExportFormat,
	error,
) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return model.ExportFormatXLSX, nil
	}

	format := model.ExportFormat(value)
	switch format {
	case model.ExportFormatXLSX,
		model.ExportFormatCSV,
		model.ExportFormatXML,
		model.ExportFormatJSON:
		return format, nil
	default:
		return "", apperr.New(
			apperr.CodeInvalidRequest,
			fmt.Errorf(
				"format must be one of: xlsx, csv, xml, json",
			),
		)
	}
}

// encodeAccountingCSV tạo CSV RFC-style với CRLF để thân thiện với Windows.
// CSV bản chất không lưu data type/number format; nếu cần đảm bảo Text/Custom
// khi mở trong Excel thì XLSX vẫn là lựa chọn chính.
func encodeAccountingCSV(table *modulexlsx.ExportTable) (
	[]byte,
	error,
) {
	if table == nil {
		return nil, fmt.Errorf("export table is required")
	}

	var buffer bytes.Buffer

	// UTF-8 BOM giúp Microsoft Excel trên Windows nhận đúng tiếng Việt khi người
	// dùng mở file CSV bằng double-click. BOM không thay đổi dữ liệu trong cell.
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	if err := writer.Write(table.Columns); err != nil {
		return nil, fmt.Errorf(
			"write csv header: %w",
			err,
		)
	}
	for _, row := range table.Rows {
		if err := writer.Write(row); err != nil {
			return nil, fmt.Errorf(
				"write csv row: %w",
				err,
			)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf(
			"flush csv: %w",
			err,
		)
	}
	return buffer.Bytes(), nil
}

// encodeAccountingJSON giữ mọi cell dưới dạng JSON string. Đây là chủ ý để MST,
// số hóa đơn và decimal không bị JSON consumer/Go chuyển qua floating point.
func encodeAccountingJSON(
	table *modulexlsx.ExportTable,
	direction model.InvoiceDirection,
	fromDate string,
	toDate string,
) (
	[]byte,
	error,
) {
	if table == nil {
		return nil, fmt.Errorf("export table is required")
	}

	payload := accountingExportPayload{
		Version:   accountingExportVersion,
		Direction: direction,
		FromDate:  fromDate,
		ToDate:    toDate,
		Columns:   buildAccountingExportColumns(table.Columns),
		Rows:      table.Rows,
	}
	data, err := json.MarshalIndent(
		payload,
		"",
		"  ",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"encode json export: %w",
			err,
		)
	}
	return append(
		data,
		'\n',
	), nil
}

// encodeAccountingXML là XML dữ liệu do EIF tạo để trao đổi/import hệ thống.
// Đây KHÔNG phải XML hóa đơn điện tử gốc có chữ ký số của cơ quan thuế/người bán.
func encodeAccountingXML(
	table *modulexlsx.ExportTable,
	direction model.InvoiceDirection,
	fromDate string,
	toDate string,
) (
	[]byte,
	error,
) {
	if table == nil {
		return nil, fmt.Errorf("export table is required")
	}

	columns := buildAccountingExportColumns(table.Columns)
	payload := accountingXMLExport{
		Version:   accountingExportVersion,
		Direction: direction,
		FromDate:  fromDate,
		ToDate:    toDate,
		Columns: accountingXMLColumns{
			Items: make(
				[]accountingXMLColumn,
				0,
				len(columns),
			),
		},
		Rows: accountingXMLRows{
			Items: make(
				[]accountingXMLRow,
				0,
				len(table.Rows),
			),
		},
	}

	for index, column := range columns {
		payload.Columns.Items = append(
			payload.Columns.Items,
			accountingXMLColumn{
				Index: index + 1,
				Name:  column.Name,
				Type:  column.Type,
			},
		)
	}

	for rowIndex, row := range table.Rows {
		xmlRow := accountingXMLRow{
			Index: rowIndex + 1,
			Cells: make(
				[]accountingXMLCell,
				0,
				len(columns),
			),
		}
		for columnIndex, column := range columns {
			value := ""
			if columnIndex < len(row) {
				value = row[columnIndex]
			}
			xmlRow.Cells = append(
				xmlRow.Cells,
				accountingXMLCell{
					Column: column.Name,
					Type:   column.Type,
					Value:  value,
				},
			)
		}
		payload.Rows.Items = append(
			payload.Rows.Items,
			xmlRow,
		)
	}

	data, err := xml.MarshalIndent(
		payload,
		"",
		"  ",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"encode xml export: %w",
			err,
		)
	}

	result := make(
		[]byte,
		0,
		len(xml.Header)+len(data)+1,
	)
	result = append(
		result,
		[]byte(xml.Header)...,
	)
	result = append(
		result,
		data...,
	)
	result = append(
		result,
		'\n',
	)
	return result, nil
}

func buildAccountingExportColumns(headers []string) []accountingExportColumn {
	columns := make(
		[]accountingExportColumn,
		0,
		len(headers),
	)
	for _, header := range headers {
		columnType := "text"
		if isAccountingAmountHeader(header) {
			columnType = "decimal"
		}
		columns = append(
			columns,
			accountingExportColumn{
				Name: header,
				Type: columnType,
			},
		)
	}
	return columns
}

func isAccountingAmountHeader(header string) bool {
	key := strings.ToLower(
		strings.Join(
			strings.Fields(header),
			" ",
		),
	)
	switch key {
	case "tổng tiền chưa thuế",
		"tổng tiền thuế",
		"tổng tiền chiết khấu thương mại",
		"tổng tiền thanh toán":
		return true
	default:
		return false
	}
}
