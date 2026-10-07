package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	stylesPath   = "xl/styles.xml"
	workbookPath = "xl/workbook.xml"

	// exportAmountNumberFormat là Custom Number Format dùng riêng cho các cột tiền.
	//
	// Excel chia custom format thành ba phần, ngăn cách bởi dấu ';':
	//
	//	#,###.0#####   -> số dương
	//	-#,###.0#####  -> số âm
	//	0              -> số 0
	//
	// Ví dụ:
	//
	//	2092593   -> 2,092,593.0
	//	167407.25 -> 167,407.25
	//	-1250.5   -> -1,250.5
	//	0         -> 0
	//
	// Đây chỉ là cách HIỂN THỊ. Cell vẫn là numeric cell để kế toán có thể
	// SUM, FILTER, Pivot... bình thường.
	exportAmountNumberFormat = "#,###.0#####;-#,###.0#####;0"

	// Excel lưu numeric cell bằng số thực và chỉ đảm bảo khoảng 15 chữ số có nghĩa.
	// Với dữ liệu kế toán, tuyệt đối không âm thầm đưa một số vượt giới hạn này vào
	// numeric cell vì Excel có thể làm tròn mà người dùng không nhận ra.
	excelMaxSignificantDigits = 15

	// Custom format phía trên hiển thị tối đa 6 chữ số phần thập phân:
	// 1 chữ số bắt buộc sau dấu '.' + 5 chữ số tùy chọn '#'.
	exportAmountMaxDecimalPlaces = 6
)

var (
	cellRefAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+r[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	cellStyleAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+s[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	xmlCountAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+count[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	xmlNumFmtIDAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+numFmtId[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	xmlApplyNumberFormatAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+applyNumberFormat[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	xmlFormatCodeAttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+formatCode[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	date1904AttributePattern = regexp.MustCompile(
		`(?i)[ \t\r\n]+date1904[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`,
	)
	numFmtIDPattern = regexp.MustCompile(`(?i)numFmtId[ \t\r\n]*=[ \t\r\n]*["']([0-9]+)["']`)
)

// Các header dưới đây phải được xuất dưới dạng Text.
// Dùng normalizeMergeHeaderKey() để việc so khớp không phụ thuộc viết hoa/thường
// hoặc việc GDT chèn xuống dòng/khoảng trắng trong header.
var invoiceTextHeaders = map[string]struct{}{
	normalizeMergeHeaderKey("STT"):                               {},
	normalizeMergeHeaderKey("Ký hiệu mẫu số"):                    {},
	normalizeMergeHeaderKey("Ký hiệu mẫu số hóa đơn"):            {},
	normalizeMergeHeaderKey("Ký hiệu hóa đơn"):                   {},
	normalizeMergeHeaderKey("Số hóa đơn"):                        {},
	normalizeMergeHeaderKey("Ngày lập"):                          {},
	normalizeMergeHeaderKey("MST người bán/MST người xuất hàng"): {},
	normalizeMergeHeaderKey("Tên người bán/Tên người xuất hàng"): {},
	normalizeMergeHeaderKey("MST người mua/MST người nhận hàng"): {},
	normalizeMergeHeaderKey("Tên người mua/Tên người nhận hàng"): {},
	normalizeMergeHeaderKey("Địa chỉ người mua"):                 {},
	normalizeMergeHeaderKey("Căn cước công dân"):                 {},
	normalizeMergeHeaderKey("Trạng thái hóa đơn"):                {},
	normalizeMergeHeaderKey("Kết quả kiểm tra hóa đơn"):          {},
}

// Các header dưới đây phải là numeric cell và dùng Custom Number Format.
var invoiceAmountHeaders = map[string]struct{}{
	normalizeMergeHeaderKey("Tổng tiền chưa thuế"):             {},
	normalizeMergeHeaderKey("Tổng tiền thuế"):                  {},
	normalizeMergeHeaderKey("Tổng tiền chiết khấu thương mại"): {},
	normalizeMergeHeaderKey("Tổng tiền thanh toán"):            {},
}

// Ngày lập được ép thành Text giống yêu cầu nghiệp vụ. Nếu workbook nguồn lưu
// ngày dưới dạng Excel serial number thay vì chuỗi DD/MM/YYYY, EIF phải
// materialize giá trị hiển thị trước khi đổi cell sang inlineStr; nếu không
// Excel serial như 46000 sẽ bị biến thành text "46000" và làm sai dữ liệu.
var invoiceDateHeaders = map[string]struct{}{
	normalizeMergeHeaderKey("Ngày lập"): {},
}

// ExportTable là dạng bảng trung gian dùng khi chuyển workbook cuối cùng sang
// CSV/XML/JSON. Giá trị cell được giữ dưới dạng string để không đi qua float64.
// Điều này đặc biệt quan trọng với MST, số hóa đơn và các giá trị tiền cần giữ
// nguyên biểu diễn thập phân từ workbook nguồn.
type ExportTable struct {
	Columns []string
	Rows    [][]string
}

// FormatInvoiceExport áp dụng quy tắc dữ liệu kế toán cho workbook cuối cùng.
//
// Luồng xử lý:
//
//	merged.xlsx
//	    ↓
//	NormalizeNumericDisplay()
//	    ↓
//	xác định cột theo header
//	    ↓
//	Text columns  -> inlineStr
//	Money columns -> numeric + Custom Number Format
//	    ↓
//	validate XML
//	    ↓
//	final.xlsx
//
// Hàm sửa raw worksheet XML thay vì unmarshal rồi marshal toàn bộ worksheet.
// Cách này giữ nguyên namespace/prefix OOXML do HDDT GDT tạo và tránh lặp lại lỗi
// Excel báo "Replaced Part: /xl/worksheets/sheet1.xml".
func FormatInvoiceExport(body []byte) (
	[]byte,
	error,
) {
	normalizedBody, err := NormalizeNumericDisplay(body)
	if err != nil {
		return nil, err
	}

	workbook, err := readMergeWorkbook(normalizedBody)
	if err != nil {
		return nil, fmt.Errorf(
			"read export workbook: %w",
			err,
		)
	}

	textColumns, amountColumns := buildInvoiceExportColumns(workbook.Header)
	if len(textColumns) == 0 && len(amountColumns) == 0 {
		return normalizedBody, nil
	}
	dateColumns := buildInvoiceDateColumns(workbook.Header)

	parts, err := readWorkbookParts(
		normalizedBody,
		worksheetPath,
		stylesPath,
		workbookPath,
	)
	if err != nil {
		return nil, err
	}

	cellValues := resolveWorkbookCellValues(workbook)
	cellNumberFormats, err := readCellNumberFormats(parts[stylesPath])
	if err != nil {
		return nil, err
	}
	usesDate1904 := doesWorkbookUse1904DateSystem(parts[workbookPath])

	amountStyles, err := collectAmountCellStyles(
		parts[worksheetPath],
		workbook.HeaderRow,
		amountColumns,
	)
	if err != nil {
		return nil, err
	}

	styles, styleMapping, err := addAccountingAmountStyles(
		parts[stylesPath],
		amountStyles,
	)
	if err != nil {
		return nil, err
	}

	sheet, err := formatInvoiceWorksheet(
		parts[worksheetPath],
		workbook.HeaderRow,
		textColumns,
		dateColumns,
		amountColumns,
		cellValues,
		cellNumberFormats,
		usesDate1904,
		styleMapping,
	)
	if err != nil {
		return nil, err
	}

	if err := validateWorksheetXML(sheet); err != nil {
		return nil, fmt.Errorf(
			"validate formatted worksheet: %w",
			err,
		)
	}
	if err := validateWorksheetXML(styles); err != nil {
		return nil, fmt.Errorf(
			"validate formatted styles: %w",
			err,
		)
	}

	return replaceWorkbookParts(
		normalizedBody,
		map[string][]byte{
			worksheetPath: sheet,
			stylesPath:    styles,
		},
	)
}

// ReadExportTable đọc dữ liệu đã merge từ XLSX về bảng chuỗi theo đúng thứ tự
// cột trên worksheet. Hàm này dùng cho CSV/XML/JSON để mọi format xuất ra đều
// lấy từ cùng một nguồn dữ liệu cuối, tránh mỗi format có một logic riêng.
func ReadExportTable(body []byte) (
	*ExportTable,
	error,
) {
	workbook, err := readMergeWorkbook(body)
	if err != nil {
		return nil, err
	}

	// CSV/XML/JSON cũng phải nhận đúng giá trị hiển thị của các cột Text.
	// Ví dụ ngày Excel serial hoặc MST dùng format 0000000000 cần được
	// materialize trước khi mất metadata style khi chuyển sang format text.
	parts, err := readWorkbookParts(
		body,
		stylesPath,
		workbookPath,
	)
	if err != nil {
		return nil, err
	}
	cellNumberFormats, err := readCellNumberFormats(parts[stylesPath])
	if err != nil {
		return nil, err
	}
	usesDate1904 := doesWorkbookUse1904DateSystem(parts[workbookPath])
	textColumns, _ := buildInvoiceExportColumns(workbook.Header)
	dateColumns := buildInvoiceDateColumns(workbook.Header)

	type orderedColumn struct {
		Column string
		Header string
	}

	columns := make(
		[]orderedColumn,
		0,
		len(workbook.Header),
	)
	for column, header := range workbook.Header {
		columns = append(
			columns,
			orderedColumn{
				Column: column,
				Header: header,
			},
		)
	}
	sort.SliceStable(
		columns,
		func(i, j int) bool {
			return getMergeColumnNumber(columns[i].Column) <
				getMergeColumnNumber(columns[j].Column)
		},
	)

	result := &ExportTable{
		Columns: make(
			[]string,
			0,
			len(columns),
		),
		Rows: make(
			[][]string,
			0,
			len(workbook.Rows)-workbook.HeaderIndex-1,
		),
	}
	for _, column := range columns {
		header := strings.TrimSpace(column.Header)
		if header == "" {
			header = "Cột " + column.Column
		}
		result.Columns = append(
			result.Columns,
			header,
		)
	}

	for rowIndex := workbook.HeaderIndex + 1; rowIndex < len(workbook.Rows); rowIndex++ {
		row := workbook.Rows[rowIndex]
		if !isMergeRowPopulated(
			row,
			workbook.SharedStrings,
		) {
			continue
		}

		byColumn := make(
			map[string]string,
			len(row.Cells),
		)
		for _, cell := range row.Cells {
			column := getMergeCellColumn(cell.Ref)
			if column == "" {
				continue
			}

			value := getMergeCellText(
				cell,
				workbook.SharedStrings,
			)
			if _, asText := textColumns[column]; asText {
				if numericValue, numeric := getMergeNumericCellValue(cell); numeric {
					style := getMergeCellStyleIndex(cell.Style)
					_, isDate := dateColumns[column]
					value = materializeNumericTextValue(
						numericValue,
						cellNumberFormats[style],
						isDate,
						usesDate1904,
					)
				}
			}
			byColumn[column] = value
		}

		values := make(
			[]string,
			len(columns),
		)
		for columnIndex, column := range columns {
			values[columnIndex] = byColumn[column.Column]
		}
		result.Rows = append(
			result.Rows,
			values,
		)
	}

	return result, nil
}

func buildInvoiceExportColumns(header map[string]string) (
	map[string]struct{},
	map[string]struct{},
) {
	textColumns := make(map[string]struct{})
	amountColumns := make(map[string]struct{})

	for column, value := range header {
		key := normalizeMergeHeaderKey(value)
		if _, ok := invoiceTextHeaders[key]; ok {
			textColumns[column] = struct{}{}
		}
		if _, ok := invoiceAmountHeaders[key]; ok {
			amountColumns[column] = struct{}{}
		}
	}

	return textColumns, amountColumns
}

// buildInvoiceDateColumns trả về vị trí các cột ngày cần materialize thành
// chuỗi trước khi đổi data type. Tách riêng map này khỏi invoiceTextHeaders để
// phần lớn cột Text không phải đi qua logic Excel serial date.
func buildInvoiceDateColumns(header map[string]string) map[string]struct{} {
	columns := make(map[string]struct{})
	for column, value := range header {
		if _, ok := invoiceDateHeaders[normalizeMergeHeaderKey(value)]; ok {
			columns[column] = struct{}{}
		}
	}
	return columns
}

func resolveWorkbookCellValues(workbook *mergeWorkbook) map[string]string {
	values := make(map[string]string)
	if workbook == nil {
		return values
	}

	for _, row := range workbook.Rows {
		for _, cell := range row.Cells {
			if strings.TrimSpace(cell.Ref) == "" {
				continue
			}
			values[cell.Ref] = getMergeCellText(
				cell,
				workbook.SharedStrings,
			)
		}
	}
	return values
}

// collectAmountCellStyles quét worksheet để lấy toàn bộ style index đang được
// các cell tiền sử dụng. Không giả định GDT chỉ có một style vì Standard/SCO
// hoặc các template khác nhau có thể dùng border/alignment khác nhau.
// Mỗi style gốc sau đó sẽ được clone và chỉ thay Number Format.
func collectAmountCellStyles(
	sheet []byte,
	headerRow int,
	amountColumns map[string]struct{},
) (
	[]int,
	error,
) {
	text := string(sheet)
	styles := make(map[int]struct{})
	cursor := 0

	for {
		cellStart := findNextCellStart(
			text,
			cursor,
		)
		if cellStart < 0 {
			break
		}

		openEndRelative := strings.IndexByte(
			text[cellStart:],
			'>',
		)
		if openEndRelative < 0 {
			return nil, fmt.Errorf("worksheet cell opening tag is invalid")
		}
		openEnd := cellStart + openEndRelative
		startTag := text[cellStart : openEnd+1]
		cursor = openEnd + 1

		ref := getRawCellRef(startTag)
		column, rowNumber := splitCellRef(ref)
		if rowNumber <= headerRow {
			continue
		}
		if _, ok := amountColumns[column]; !ok {
			continue
		}
		if strings.HasSuffix(
			strings.TrimSpace(startTag),
			"/>",
		) {
			continue
		}

		style, err := getRawCellStyle(startTag)
		if err != nil {
			return nil, fmt.Errorf(
				"read style for cell %s: %w",
				ref,
				err,
			)
		}
		styles[style] = struct{}{}
	}

	result := make(
		[]int,
		0,
		len(styles),
	)
	for style := range styles {
		result = append(
			result,
			style,
		)
	}
	sort.Ints(result)
	return result, nil
}

// formatInvoiceWorksheet sửa từng <c>...</c> trên raw sheet XML.
// Chỉ những cell nằm sau header và thuộc danh sách cột nghiệp vụ mới bị đổi;
// mọi cell khác được copy byte-for-byte để hạn chế tối đa thay đổi OOXML nguồn.
func formatInvoiceWorksheet(
	sheet []byte,
	headerRow int,
	textColumns map[string]struct{},
	dateColumns map[string]struct{},
	amountColumns map[string]struct{},
	cellValues map[string]string,
	cellNumberFormats map[int]string,
	usesDate1904 bool,
	styleMapping map[int]int,
) (
	[]byte,
	error,
) {
	text := string(sheet)
	var out strings.Builder
	out.Grow(len(text) + 512)

	cursor := 0
	for {
		cellStart := findNextCellStart(
			text,
			cursor,
		)
		if cellStart < 0 {
			out.WriteString(text[cursor:])
			break
		}

		out.WriteString(text[cursor:cellStart])

		openEndRelative := strings.IndexByte(
			text[cellStart:],
			'>',
		)
		if openEndRelative < 0 {
			return nil, fmt.Errorf("worksheet cell opening tag is invalid")
		}
		openEnd := cellStart + openEndRelative
		startTag := text[cellStart : openEnd+1]

		if strings.HasSuffix(
			strings.TrimSpace(startTag),
			"/>",
		) {
			out.WriteString(startTag)
			cursor = openEnd + 1
			continue
		}

		closeStartRelative := strings.Index(
			text[openEnd+1:],
			"</c>",
		)
		if closeStartRelative < 0 {
			return nil, fmt.Errorf("worksheet cell closing tag is missing")
		}
		closeStart := openEnd + 1 + closeStartRelative
		cellEnd := closeStart + len("</c>")
		inner := text[openEnd+1 : closeStart]

		ref := getRawCellRef(startTag)
		column, rowNumber := splitCellRef(ref)
		if ref == "" || rowNumber <= headerRow {
			out.WriteString(text[cellStart:cellEnd])
			cursor = cellEnd
			continue
		}

		if _, ok := textColumns[column]; ok {
			// Text được ghi thành inlineStr để Excel không tự suy luận lại kiểu.
			// Nhờ vậy MST, số hóa đơn, CCCD... không bị scientific notation và
			// không bị mất số 0 đầu nếu workbook nguồn đã chứa đúng giá trị text.
			value := cellValues[ref]
			if numericValue, numeric := getRawNumericCellValue(
				startTag,
				inner,
			); numeric {
				style, styleErr := getRawCellStyle(startTag)
				if styleErr != nil {
					return nil, fmt.Errorf(
						"read style for text cell %s: %w",
						ref,
						styleErr,
					)
				}

				_, isDate := dateColumns[column]
				value = materializeNumericTextValue(
					numericValue,
					cellNumberFormats[style],
					isDate,
					usesDate1904,
				)
			}

			out.WriteString(
				setRawCellType(
					startTag,
					"inlineStr",
				),
			)
			out.WriteString(`<is><t xml:space="preserve">`)
			out.WriteString(escapeText(value))
			out.WriteString(`</t></is></c>`)
			cursor = cellEnd
			continue
		}

		if _, ok := amountColumns[column]; ok {
			value := strings.TrimSpace(cellValues[ref])
			if value == "" {
				out.WriteString(text[cellStart:cellEnd])
				cursor = cellEnd
				continue
			}

			// Không dùng strconv.ParseFloat/float64 ở đây. Giá trị tiền được xử lý
			// dưới dạng decimal literal để không phát sinh sai số trước khi Excel
			// nhận numeric value.
			decimal, err := normalizeAccountingDecimal(value)
			if err != nil {
				return nil, fmt.Errorf(
					"invalid accounting value at cell %s: %w",
					ref,
					err,
				)
			}

			style, err := getRawCellStyle(startTag)
			if err != nil {
				return nil, fmt.Errorf(
					"read style for cell %s: %w",
					ref,
					err,
				)
			}
			formattedStyle, ok := styleMapping[style]
			if !ok {
				return nil, fmt.Errorf(
					"accounting number style for cell %s is missing",
					ref,
				)
			}

			startTag = removeRawCellType(startTag)
			startTag = setRawCellStyle(
				startTag,
				formattedStyle,
			)
			out.WriteString(startTag)
			out.WriteString(`<v>`)
			out.WriteString(escapeText(decimal))
			out.WriteString(`</v></c>`)
			cursor = cellEnd
			continue
		}

		out.WriteString(text[cellStart:cellEnd])
		cursor = cellEnd
	}

	return []byte(out.String()), nil
}

// getMergeNumericCellValue trả raw numeric literal từ struct đã decode. Khác với
// strconv.ParseFloat, hàm chỉ phân loại kiểu cell và giữ nguyên chuỗi số để các
// mã định danh/decimal không bị làm tròn trong Go.
func getMergeNumericCellValue(cell mergeCell) (
	string,
	bool,
) {
	cellType := strings.TrimSpace(cell.Type)
	if cellType != "" && !strings.EqualFold(
		cellType,
		"n",
	) {
		return "", false
	}
	value := strings.TrimSpace(cell.Value)
	if value == "" {
		return "", false
	}
	return value, true
}

func getMergeCellStyleIndex(value string) int {
	style, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || style < 0 {
		return 0
	}
	return style
}

// readCellNumberFormats map style index của <cellXfs> sang custom formatCode.
//
// Ta chỉ cần metadata để materialize các numeric identifier thành Text. Ví dụ
// upstream có thể lưu MST 0106324276 dưới dạng numeric value 106324276 nhưng
// style lại là "0000000000". Nếu đổi thẳng raw value sang Text thì số 0 đầu sẽ
// mất; đọc formatCode trước giúp EIF khôi phục đúng giá trị đang hiển thị.
func readCellNumberFormats(styles []byte) (
	map[int]string,
	error,
) {
	text := string(styles)
	customFormats := make(map[int]string)
	cursor := 0

	for cursor < len(text) {
		relative := strings.Index(
			text[cursor:],
			"<numFmt",
		)
		if relative < 0 {
			break
		}
		start := cursor + relative
		next := start + len("<numFmt")
		if next < len(text) && !isXMLTagBoundary(text[next]) {
			cursor = next
			continue
		}

		openEndRelative := strings.IndexByte(
			text[start:],
			'>',
		)
		if openEndRelative < 0 {
			return nil, fmt.Errorf("xlsx numFmt opening tag is invalid")
		}
		openEnd := start + openEndRelative
		tag := text[start : openEnd+1]
		cursor = openEnd + 1

		idValue := getRawAttributeValue(
			xmlNumFmtIDAttributePattern.FindString(tag),
		)
		formatCode := getRawAttributeValue(
			xmlFormatCodeAttributePattern.FindString(tag),
		)
		if idValue == "" || formatCode == "" {
			continue
		}

		id, err := strconv.Atoi(idValue)
		if err != nil || id < 0 {
			return nil, fmt.Errorf(
				"invalid xlsx numFmtId %q",
				idValue,
			)
		}
		customFormats[id] = formatCode
	}

	_, cellXfsOpenEnd, cellXfsCloseStart, ok := findRawElementBounds(
		text,
		"cellXfs",
	)
	if !ok {
		return nil, fmt.Errorf("xlsx styles cellXfs is missing")
	}

	xfs := splitRawXFs(text[cellXfsOpenEnd:cellXfsCloseStart])
	result := make(
		map[int]string,
		len(xfs),
	)
	for style, xf := range xfs {
		idValue := getRawAttributeValue(
			xmlNumFmtIDAttributePattern.FindString(xf),
		)
		if idValue == "" {
			continue
		}
		id, err := strconv.Atoi(idValue)
		if err != nil || id < 0 {
			return nil, fmt.Errorf(
				"invalid xlsx cellXf numFmtId %q",
				idValue,
			)
		}
		if formatCode, exists := customFormats[id]; exists {
			result[style] = formatCode
		}
	}

	return result, nil
}

// doesWorkbookUse1904DateSystem đọc date system của Excel. Workbook mặc định dùng
// hệ 1900; một số workbook (đặc biệt file tạo từ hệ sinh thái Apple cũ) có thể
// dùng hệ 1904. Chọn sai hệ sẽ làm ngày lệch đúng 1462 ngày.
func doesWorkbookUse1904DateSystem(workbook []byte) bool {
	attribute := date1904AttributePattern.FindString(string(workbook))
	value := strings.ToLower(
		strings.TrimSpace(
			getRawAttributeValue(attribute),
		),
	)
	return value == "1" || value == "true"
}

// materializeNumericTextValue chuyển numeric cell thành đúng chuỗi người dùng
// cần nhìn thấy trước khi cell được đổi sang inlineStr.
//
// Hai trường hợp cần xử lý đặc biệt:
//
//  1. Ngày lập lưu bằng Excel serial -> đổi thành DD/MM/YYYY.
//  2. Identifier dùng format bắt buộc số 0 đầu (vd. 0000000000) -> pad số 0
//     trước khi bỏ number format.
//
// Hàm không dùng float64 cho identifier thông thường; scientific notation được
// mở rộng bằng thao tác chuỗi để không phát sinh rounding.
func materializeNumericTextValue(
	value string,
	formatCode string,
	isDate bool,
	usesDate1904 bool,
) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if strings.ContainsAny(
		value,
		"eE",
	) {
		if expanded, ok := expandScientificNotation(value); ok {
			value = expanded
		}
	}

	if isDate {
		if formatted, ok := formatExcelDate(
			value,
			usesDate1904,
		); ok {
			return formatted
		}
	}

	width := getRequiredIntegerDigits(formatCode)
	if width > 0 {
		sign := ""
		digits := value
		if strings.HasPrefix(
			digits,
			"+",
		) || strings.HasPrefix(
			digits,
			"-",
		) {
			sign = digits[:1]
			digits = digits[1:]
		}

		if isDecimalDigits(digits) && len(digits) < width {
			return sign + strings.Repeat(
				"0",
				width-len(digits),
			) + digits
		}
	}

	// Identifier/STT thường là integer nhưng một số producer có thể ghi "1.0".
	// Nếu phần thập phân chỉ gồm số 0, bỏ nó để giá trị Text khớp cách Excel
	// hiển thị integer thông thường. Không đụng vào decimal có giá trị thật.
	if dot := strings.IndexByte(
		value,
		'.',
	); dot >= 0 {
		fraction := value[dot+1:]
		if fraction != "" && strings.Trim(
			fraction,
			"0",
		) == "" {
			return value[:dot]
		}
	}

	return value
}

// getRequiredIntegerDigits lấy số lượng placeholder '0' bắt buộc ở phần nguyên
// của custom number format. Với "0000000000" kết quả là 10. Các format có
// literal/phần điều kiện phức tạp được bỏ qua thay vì đoán để tránh sửa sai dữ
// liệu định danh.
func getRequiredIntegerDigits(formatCode string) int {
	formatCode = strings.TrimSpace(formatCode)
	if formatCode == "" {
		return 0
	}

	section := strings.SplitN(
		formatCode,
		";",
		2,
	)[0]
	if decimal := strings.IndexByte(
		section,
		'.',
	); decimal >= 0 {
		section = section[:decimal]
	}

	if strings.ContainsAny(
		section,
		`"'[]\\@`,
	) {
		return 0
	}

	required := 0
	for _, r := range section {
		switch r {
		case '0':
			required++
		case '#', ',', ' ', '\t':
			// Các ký tự placeholder/group separator này không làm thay đổi số
			// lượng chữ số bắt buộc.
		default:
			return 0
		}
	}
	return required
}

// formatExcelDate materialize Excel serial number thành ngày hiển thị. ParseFloat
// chỉ được dùng cho serial date (giá trị nhỏ, không phải số tiền), nên không tạo
// rủi ro precision kế toán mà phần amount đang chủ động tránh.
func formatExcelDate(
	value string,
	usesDate1904 bool,
) (
	string,
	bool,
) {
	// Một số producer ghi ngày dạng YYYYMMDD numeric thay vì Excel serial.
	// Nhận dạng riêng để không hiểu nhầm 20260302 thành serial date.
	if len(value) == 8 && isDecimalDigits(value) {
		if parsed, err := time.Parse(
			"20060102",
			value,
		); err == nil {
			return parsed.Format("02/01/2006"), true
		}
	}

	serial, err := strconv.ParseFloat(
		value,
		64,
	)
	if err != nil || math.IsNaN(serial) || math.IsInf(
		serial,
		0,
	) {
		return "", false
	}

	// 2,958,465 là vùng ngày tối đa xấp xỉ 31/12/9999 trong date system 1900.
	// Giới hạn giúp tránh biến nhầm một mã số lớn thành ngày.
	if serial < 0 || serial > 2958465 {
		return "", false
	}

	days := int(math.Floor(serial))
	var date time.Time
	if usesDate1904 {
		date = time.Date(
			1904,
			time.January,
			1,
			0,
			0,
			0,
			0,
			time.UTC,
		).AddDate(
			0,
			0,
			days,
		)
	} else {
		// Excel 1900 date system cố tình tương thích bug lịch sử coi 1900 là
		// leap year. Với serial >= 60 phải trừ một ngày giả 29/02/1900.
		if days >= 60 {
			days--
		}
		date = time.Date(
			1899,
			time.December,
			31,
			0,
			0,
			0,
			0,
			time.UTC,
		).AddDate(
			0,
			0,
			days,
		)
	}

	return date.Format("02/01/2006"), true
}

// normalizeAccountingDecimal chuẩn hóa số tiền bằng thao tác chuỗi, không parse
// qua float64. Mục tiêu là kiểm tra được chính xác số chữ số trước khi giao giá
// trị cho Excel và fail sớm nếu Excel không thể giữ độ chính xác mong muốn.
func normalizeAccountingDecimal(value string) (
	string,
	error,
) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}

	// GDT/Excel có thể đưa giá trị về dạng hiển thị có dấu phân cách hàng nghìn.
	// Dấu ',' ở đây chỉ được coi là thousands separator; dấu '.' là decimal point.
	value = strings.ReplaceAll(
		value,
		",",
		"",
	)

	if strings.ContainsAny(
		value,
		"eE",
	) {
		expanded, ok := expandScientificNotation(value)
		if !ok {
			return "", fmt.Errorf(
				"invalid scientific notation %q",
				value,
			)
		}
		value = expanded
	}

	negative := false
	if strings.HasPrefix(
		value,
		"+",
	) || strings.HasPrefix(
		value,
		"-",
	) {
		negative = value[0] == '-'
		value = value[1:]
	}
	if value == "" || strings.Count(
		value,
		".",
	) > 1 {
		return "", fmt.Errorf("invalid decimal number")
	}

	parts := strings.SplitN(
		value,
		".",
		2,
	)
	integerPart := parts[0]
	fractionPart := ""
	if len(parts) == 2 {
		fractionPart = parts[1]
	}
	if integerPart == "" {
		integerPart = "0"
	}
	if !isDecimalDigits(integerPart) || !isDecimalDigitsOrEmpty(fractionPart) {
		return "", fmt.Errorf("invalid decimal number")
	}

	integerPart = strings.TrimLeft(
		integerPart,
		"0",
	)
	if integerPart == "" {
		integerPart = "0"
	}

	// Trailing zero không thay đổi giá trị số. Bỏ chúng trước khi kiểm tra số chữ
	// số thập phân giúp 1.2300000 vẫn được hiểu chính xác là 1.23 thay vì báo lỗi.
	fractionPart = strings.TrimRight(
		fractionPart,
		"0",
	)
	if len(fractionPart) > exportAmountMaxDecimalPlaces {
		return "", fmt.Errorf(
			"value has %d decimal places, maximum displayed safely is %d",
			len(fractionPart),
			exportAmountMaxDecimalPlaces,
		)
	}

	canonical := integerPart
	if fractionPart != "" {
		canonical += "." + fractionPart
	}
	if countSignificantDecimalDigits(canonical) > excelMaxSignificantDigits {
		return "", fmt.Errorf(
			"value exceeds Excel's %d significant digit precision",
			excelMaxSignificantDigits,
		)
	}

	if negative && canonical != "0" {
		canonical = "-" + canonical
	}
	return canonical, nil
}

func isDecimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isDecimalDigitsOrEmpty(value string) bool {
	if value == "" {
		return true
	}
	return isDecimalDigits(value)
}

func getRawCellRef(startTag string) string {
	return getRawAttributeValue(
		cellRefAttributePattern.FindString(startTag),
	)
}

func getRawCellStyle(startTag string) (
	int,
	error,
) {
	value := getRawAttributeValue(
		cellStyleAttributePattern.FindString(startTag),
	)
	if value == "" {
		return 0, nil
	}

	style, err := strconv.Atoi(value)
	if err != nil || style < 0 {
		return 0, fmt.Errorf(
			"invalid cell style %q",
			value,
		)
	}
	return style, nil
}

func splitCellRef(ref string) (
	string,
	int,
) {
	ref = strings.TrimSpace(ref)
	column := getMergeCellColumn(ref)
	if column == "" || len(column) == len(ref) {
		return column, 0
	}
	row, err := strconv.Atoi(ref[len(column):])
	if err != nil {
		return column, 0
	}
	return column, row
}

func removeRawCellType(startTag string) string {
	if location := cellTypeAttributePattern.FindStringIndex(startTag); location != nil {
		return startTag[:location[0]] + startTag[location[1]:]
	}
	return startTag
}

func setRawCellStyle(
	startTag string,
	style int,
) string {
	return setRawAttribute(
		startTag,
		cellStyleAttributePattern,
		"s",
		strconv.Itoa(style),
	)
}

func getRawAttributeValue(attribute string) string {
	if attribute == "" {
		return ""
	}
	equals := strings.IndexByte(
		attribute,
		'=',
	)
	if equals < 0 {
		return ""
	}

	value := strings.TrimSpace(attribute[equals+1:])
	if len(value) < 2 {
		return ""
	}
	quote := value[0]
	if (quote != '\'' && quote != '"') || value[len(value)-1] != quote {
		return ""
	}
	return value[1 : len(value)-1]
}

// setRawAttribute thay hoặc thêm một attribute trên raw XML opening tag.
// Hàm xử lý cả tag thường <xf ...> lẫn self-closing tag <xf .../>; nếu chèn
// attribute sai vị trí trước dấu '>' nhưng sau dấu '/' thì XML sẽ hỏng.
func setRawAttribute(
	tag string,
	pattern *regexp.Regexp,
	name string,
	value string,
) string {
	if location := pattern.FindStringIndex(tag); location != nil {
		return tag[:location[0]] + ` ` + name + `="` + escapeAttribute(value) + `"` + tag[location[1]:]
	}

	end := strings.LastIndexByte(
		tag,
		'>',
	)
	if end < 0 {
		return tag
	}

	insertAt := end
	for insertAt > 0 && (tag[insertAt-1] == ' ' || tag[insertAt-1] == '\t' || tag[insertAt-1] == '\r' || tag[insertAt-1] == '\n') {
		insertAt--
	}
	if insertAt > 0 && tag[insertAt-1] == '/' {
		insertAt--
	}

	return tag[:insertAt] + ` ` + name + `="` + escapeAttribute(value) + `"` + tag[insertAt:]
}

// addAccountingAmountStyles không sửa trực tiếp style gốc của workbook.
// Thay vào đó, mỗi style đang dùng ở cột tiền được clone rồi gắn custom numFmt.
// Cách clone giữ nguyên font/fill/border/alignment mà GDT đã thiết kế và tránh
// làm thay đổi các cell khác đang dùng chung style gốc.
func addAccountingAmountStyles(
	styles []byte,
	sourceStyles []int,
) (
	[]byte,
	map[int]int,
	error,
) {
	mapping := make(map[int]int)
	if len(sourceStyles) == 0 {
		return append(
			[]byte(nil),
			styles...,
		), mapping, nil
	}

	text := string(styles)
	cellXfsStart, cellXfsOpenEnd, cellXfsCloseStart, ok := findRawElementBounds(
		text,
		"cellXfs",
	)
	if !ok {
		return nil, nil, fmt.Errorf("xlsx styles cellXfs is missing")
	}

	cellXfsContent := text[cellXfsOpenEnd:cellXfsCloseStart]
	xfs := splitRawXFs(cellXfsContent)
	if len(xfs) == 0 {
		return nil, nil, fmt.Errorf("xlsx styles cellXfs is empty")
	}

	for _, style := range sourceStyles {
		if style < 0 || style >= len(xfs) {
			return nil, nil, fmt.Errorf(
				"xlsx cell style %d is out of range",
				style,
			)
		}
	}

	numFmtID := nextCustomNumberFormatID(text)
	text, err := addNumberFormat(
		text,
		numFmtID,
		exportAmountNumberFormat,
	)
	if err != nil {
		return nil, nil, err
	}

	// addNumberFormat() có thể làm vị trí cellXfs dịch chuyển, vì vậy phải tìm
	// bounds lại trên chuỗi mới trước khi append các style clone.
	cellXfsStart, cellXfsOpenEnd, cellXfsCloseStart, ok = findRawElementBounds(
		text,
		"cellXfs",
	)
	if !ok {
		return nil, nil, fmt.Errorf("xlsx styles cellXfs is missing after number format update")
	}
	cellXfsOpenTag := text[cellXfsStart:cellXfsOpenEnd]
	cellXfsContent = text[cellXfsOpenEnd:cellXfsCloseStart]
	xfs = splitRawXFs(cellXfsContent)

	var clones strings.Builder
	for _, sourceStyle := range sourceStyles {
		clone, err := cloneXFWithNumberFormat(
			xfs[sourceStyle],
			numFmtID,
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"clone xlsx style %d: %w",
				sourceStyle,
				err,
			)
		}
		mapping[sourceStyle] = len(xfs) + len(mapping)
		clones.WriteString(clone)
	}

	updatedOpenTag := setRawAttribute(
		cellXfsOpenTag,
		xmlCountAttributePattern,
		"count",
		strconv.Itoa(len(xfs)+len(sourceStyles)),
	)

	text = text[:cellXfsStart] +
		updatedOpenTag +
		cellXfsContent +
		clones.String() +
		text[cellXfsCloseStart:]

	return []byte(text), mapping, nil
}

// nextCustomNumberFormatID tìm numFmtId custom chưa được workbook sử dụng.
// OOXML dành vùng custom bắt đầu từ 164; không hard-code 164 vì file nguồn có
// thể đã dùng ID đó cho một format khác.
func nextCustomNumberFormatID(styles string) int {
	used := make(map[int]struct{})
	for _, match := range numFmtIDPattern.FindAllStringSubmatch(
		styles,
		-1,
	) {
		if len(match) != 2 {
			continue
		}
		id, err := strconv.Atoi(match[1])
		if err == nil {
			used[id] = struct{}{}
		}
	}

	for id := 164; ; id++ {
		if _, ok := used[id]; !ok {
			return id
		}
	}
}

// addNumberFormat thêm <numFmt> vào styles.xml và đồng bộ lại thuộc tính count.
// Nếu workbook chưa có <numFmts>, block được chèn ngay sau <styleSheet> để đúng
// thứ tự cấu trúc phổ biến của SpreadsheetML.
func addNumberFormat(
	styles string,
	numFmtID int,
	formatCode string,
) (
	string,
	error,
) {
	numFmtXML := `<numFmt numFmtId="` + strconv.Itoa(numFmtID) + `" formatCode="` + escapeAttribute(formatCode) + `"/>`

	start, openEnd, closeStart, ok := findRawElementBounds(
		styles,
		"numFmts",
	)
	if ok {
		openTag := styles[start:openEnd]
		count := countRawElements(
			styles[openEnd:closeStart],
			"numFmt",
		)
		openTag = setRawAttribute(
			openTag,
			xmlCountAttributePattern,
			"count",
			strconv.Itoa(count+1),
		)
		return styles[:start] +
			openTag +
			styles[openEnd:closeStart] +
			numFmtXML +
			styles[closeStart:], nil
	}

	styleSheetStart := strings.Index(
		styles,
		"<styleSheet",
	)
	if styleSheetStart < 0 {
		return "", fmt.Errorf("xlsx styleSheet is missing")
	}
	openEndRelative := strings.IndexByte(
		styles[styleSheetStart:],
		'>',
	)
	if openEndRelative < 0 {
		return "", fmt.Errorf("xlsx styleSheet opening tag is invalid")
	}
	styleSheetOpenEnd := styleSheetStart + openEndRelative + 1
	return styles[:styleSheetOpenEnd] +
		`<numFmts count="1">` + numFmtXML + `</numFmts>` +
		styles[styleSheetOpenEnd:], nil
}

// cloneXFWithNumberFormat chỉ thay numFmtId/applyNumberFormat trên opening <xf>.
// Phần alignment/protection và các thuộc tính style còn lại được giữ nguyên.
func cloneXFWithNumberFormat(
	xf string,
	numFmtID int,
) (
	string,
	error,
) {
	openEnd := strings.IndexByte(
		xf,
		'>',
	)
	if openEnd < 0 {
		return "", fmt.Errorf("xf opening tag is invalid")
	}

	openTag := xf[:openEnd+1]
	openTag = setRawAttribute(
		openTag,
		xmlNumFmtIDAttributePattern,
		"numFmtId",
		strconv.Itoa(numFmtID),
	)
	openTag = setRawAttribute(
		openTag,
		xmlApplyNumberFormatAttributePattern,
		"applyNumberFormat",
		"1",
	)
	return openTag + xf[openEnd+1:], nil
}

func findRawElementBounds(
	text string,
	name string,
) (
	int,
	int,
	int,
	bool,
) {
	start := findElementStart(
		text,
		name,
	)
	if start < 0 {
		return 0, 0, 0, false
	}

	openEndRelative := strings.IndexByte(
		text[start:],
		'>',
	)
	if openEndRelative < 0 {
		return 0, 0, 0, false
	}
	openEnd := start + openEndRelative + 1

	closeTag := "</" + name + ">"
	closeRelative := strings.Index(
		text[openEnd:],
		closeTag,
	)
	if closeRelative < 0 {
		return 0, 0, 0, false
	}
	return start, openEnd, openEnd + closeRelative, true
}

// splitRawXFs tách từng <xf> bên trong <cellXfs> nhưng vẫn giữ nguyên raw XML.
// Không dùng xml.Marshal cho styles.xml để tránh viết lại những extension/namespace
// mà template GDT có thể thêm trong tương lai.
func splitRawXFs(content string) []string {
	result := make(
		[]string,
		0,
	)
	cursor := 0

	for cursor < len(content) {
		startRelative := strings.Index(
			content[cursor:],
			"<xf",
		)
		if startRelative < 0 {
			break
		}
		start := cursor + startRelative
		next := start + len("<xf")
		if next < len(content) && !isXMLTagBoundary(content[next]) {
			cursor = next
			continue
		}

		openEndRelative := strings.IndexByte(
			content[start:],
			'>',
		)
		if openEndRelative < 0 {
			break
		}
		openEnd := start + openEndRelative
		opening := strings.TrimSpace(content[start : openEnd+1])
		if strings.HasSuffix(
			opening,
			"/>",
		) {
			result = append(
				result,
				content[start:openEnd+1],
			)
			cursor = openEnd + 1
			continue
		}

		closeRelative := strings.Index(
			content[openEnd+1:],
			"</xf>",
		)
		if closeRelative < 0 {
			break
		}
		end := openEnd + 1 + closeRelative + len("</xf>")
		result = append(
			result,
			content[start:end],
		)
		cursor = end
	}

	return result
}

func countRawElements(
	content string,
	name string,
) int {
	count := 0
	cursor := 0
	needle := "<" + name
	for cursor < len(content) {
		relative := strings.Index(
			content[cursor:],
			needle,
		)
		if relative < 0 {
			break
		}
		index := cursor + relative
		next := index + len(needle)
		if next >= len(content) || isXMLTagBoundary(content[next]) {
			count++
		}
		cursor = next
	}
	return count
}

// readWorkbookParts chỉ đọc những entry ZIP cần thiết, tránh giải nén toàn bộ XLSX.
func readWorkbookParts(
	body []byte,
	names ...string,
) (
	map[string][]byte,
	error,
) {
	reader, err := zip.NewReader(
		bytes.NewReader(body),
		int64(len(body)),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open xlsx zip: %w",
			err,
		)
	}

	wanted := make(
		map[string]struct{},
		len(names),
	)
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	result := make(
		map[string][]byte,
		len(names),
	)

	for _, file := range reader.File {
		if _, ok := wanted[file.Name]; !ok {
			continue
		}
		data, err := readMergeZipEntry(file)
		if err != nil {
			return nil, err
		}
		result[file.Name] = data
	}

	for _, name := range names {
		if len(result[name]) == 0 {
			return nil, fmt.Errorf(
				"%s is missing",
				name,
			)
		}
	}
	return result, nil
}

// replaceWorkbookParts đóng gói lại XLSX bằng cách copy nguyên tất cả entry cũ và
// chỉ thay những part có trong replacements. Metadata ZIP quan trọng của entry
// nguồn cũng được giữ lại thay vì tạo workbook hoàn toàn mới.
func replaceWorkbookParts(
	body []byte,
	replacements map[string][]byte,
) (
	[]byte,
	error,
) {
	reader, err := zip.NewReader(
		bytes.NewReader(body),
		int64(len(body)),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open source xlsx: %w",
			err,
		)
	}

	found := make(
		map[string]bool,
		len(replacements),
	)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	for _, file := range reader.File {
		data, err := readMergeZipEntry(file)
		if err != nil {
			return nil, err
		}
		if replacement, ok := replacements[file.Name]; ok {
			data = replacement
			found[file.Name] = true
		}

		header := &zip.FileHeader{
			Name:     file.Name,
			Method:   file.Method,
			Comment:  file.Comment,
			NonUTF8:  file.NonUTF8,
			Modified: file.Modified,
			Extra: append(
				[]byte(nil),
				file.Extra...,
			),
			ExternalAttrs:  file.ExternalAttrs,
			CreatorVersion: file.CreatorVersion,
			ReaderVersion:  file.ReaderVersion,
			Flags:          file.Flags,
		}

		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(data); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}
	for name := range replacements {
		if !found[name] {
			return nil, fmt.Errorf(
				"xlsx part %s is missing",
				name,
			)
		}
	}
	return buffer.Bytes(), nil
}
