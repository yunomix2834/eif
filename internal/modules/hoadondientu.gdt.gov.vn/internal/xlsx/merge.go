package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

/*
# Giải Thích:
- invoice.xlsx là 1 dạng file ZIP đặc biệt. Hoàn toàn có thể đổi
invoice.xlsx -> invoice.zip -> giải nén

# Cấu trúc bên trong
invoice.xlsx
│
├── [Content_Types].xml
├── _rels/
│   └── .rels
│
├── docProps/
│   ├── app.xml
│   └── core.xml
│
└── xl/
    ├── workbook.xml
    ├── styles.xml
    ├── sharedStrings.xml
    │
    ├── _rels/
    │   └── workbook.xml.rels
    │
    └── worksheets/
        └── sheet1.xml
- workbook.xml: mô tả workbook có những sheet nào.
- sheet1.xml: mô tả các hàng và cell trong Sheet1.
- styles.xml: mô tả font, border, fill, number format, style.
- sharedStrings.xml: chứa các chuỗi text dùng trong workbook.
- Sau đó tất cả được ZIP lại: XLSX = ZIP package + XML files + relationships + assets
XML files
   ↓
  ZIP
   ↓
 .xlsx

# Một bảng Excel đơn giản được lưu như nào ?

┌─────┬──────────────────┬─────────────┐
│ STT │ Tên công ty      │ Tổng tiền   │
├─────┼──────────────────┼─────────────┤
│  1  │ Công ty ABC      │ 1,000,000   │
│  2  │ Công ty XYZ      │ 2,000,000   │
└─────┴──────────────────┴─────────────┘

Ta có tọa độ:

A1 = STT
B1 = Tên công ty
C1 = Tổng tiền

A2 = 1
B2 = Công ty ABC
C2 = 1000000

A3 = 2
B3 = Công ty XYZ
C3 = 2000000

Trong sheet1.xml, nó sẽ có cấu trúc đại khái:

<worksheet>
    <sheetData>
        <row r="1">
            <c r="A1" t="s">
                <v>0</v>
            </c>

            <c r="B1" t="s">
                <v>1</v>
            </c>

            <c r="C1" t="s">
                <v>2</v>
            </c>
        </row>

        <row r="2">
            <c r="A2">
                <v>1</v>
            </c>

            <c r="B2" t="s">
                <v>3</v>
            </c>

            <c r="C2">
                <v>1000000</v>
            </c>
        </row>
    </sheetData>
</worksheet>

# sharedStrings.xml

Excel có thể không viết:

<c r="B2">
    <v>Công ty ABC</v>
</c>

mà lưu text trong một bảng riêng:

xl/sharedStrings.xml

Ví dụ:

<sst>
    <si><t>STT</t></si>
    <si><t>Tên công ty</t></si>
    <si><t>Tổng tiền</t></si>
    <si><t>Công ty ABC</t></si>
    <si><t>Công ty XYZ</t></si>
</sst>

=======>>

sharedStrings := []string{
	"STT",          // index 0
	"Tên công ty",  // index 1
	"Tổng tiền",    // index 2
	"Công ty ABC",  // index 3
	"Công ty XYZ",  // index 4
}

*/

const worksheetPath = "xl/worksheets/sheet1.xml"
const sharedStringsPath = "xl/sharedStrings.xml"

type mergeWorksheet struct {
	SheetData mergeSheetData `xml:"sheetData"`
}

type mergeSheetData struct {
	Rows []mergeRow `xml:"row"`
}

type mergeRow struct {
	R            int         `xml:"r,attr"`
	Height       string      `xml:"ht,attr,omitempty"`
	CustomHeight string      `xml:"customHeight,attr,omitempty"`
	Style        string      `xml:"s,attr,omitempty"`
	CustomFormat string      `xml:"customFormat,attr,omitempty"`
	Hidden       string      `xml:"hidden,attr,omitempty"`
	OutlineLevel string      `xml:"outlineLevel,attr,omitempty"`
	Cells        []mergeCell `xml:"c"`
}

type mergeCell struct {
	Ref     string            `xml:"r,attr"`
	Style   string            `xml:"s,attr,omitempty"`
	Type    string            `xml:"t,attr,omitempty"`
	Formula string            `xml:"f"`
	Value   string            `xml:"v"`
	Inline  mergeInlineString `xml:"is"`
}

type mergeInlineString struct {
	Text string         `xml:"t"`
	Runs []mergeTextRun `xml:"r"`
}

type mergeTextRun struct {
	Text string `xml:"t"`
}

type mergeSharedStrings struct {
	Items []mergeSharedString `xml:"si"`
}

type mergeSharedString struct {
	Text string         `xml:"t"`
	Runs []mergeTextRun `xml:"r"`
}

type mergeWorkbook struct {
	Body          []byte
	Sheet         []byte
	Rows          []mergeRow
	SharedStrings []string
	HeaderIndex   int
	HeaderRow     int
	STTColumn     string
	Header        map[string]string
}

// MergeSource mô tả một workbook nguồn khi cần gộp các file có schema khác nhau.
// Name được ghi vào cột "Nguồn dữ liệu" của file kết quả.
type MergeSource struct {
	Name string
	Body []byte
}

type mergeUnionColumn struct {
	Key    string
	Header string
}

type mergeWorkbookColumn struct {
	Column string
	Key    string
	Header string
}

// MergeByHeader gộp các workbook có thể có header/schema khác nhau.
//
// Merge() vẫn nên là lựa chọn đầu tiên vì giữ nguyên template/style của HDDTGDT khi
// mọi workbook có cùng schema. MergeByHeader xử lý trường hợp hai nguồn có schema khác nhau
// Standard + SCO: dữ liệu được căn theo tên header thay vì vị trí cột, tránh
// trường hợp bỏ validation rồi ghép nhầm cột.
func MergeByHeader(
	sources []MergeSource,
	title string,
) (
	[]byte,
	error,
) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("xlsx merge sources are required")
	}

	workbooks := make(
		[]*mergeWorkbook,
		0,
		len(sources),
	)
	workbookColumns := make(
		[][]mergeWorkbookColumn,
		0,
		len(sources),
	)

	columns := []mergeUnionColumn{
		{Key: "__eif_stt", Header: "STT"},
		{Key: "__eif_source", Header: "Nguồn dữ liệu"},
	}
	seen := map[string]struct{}{
		"__eif_stt":    {},
		"__eif_source": {},
	}

	for sourceIndex, source := range sources {
		workbook, err := readMergeWorkbook(source.Body)
		if err != nil {
			return nil, fmt.Errorf(
				"read xlsx source %d: %w",
				sourceIndex,
				err,
			)
		}

		descriptors := mergeWorkbookHeaderColumns(
			workbook,
			sourceIndex,
		)
		if len(descriptors) == 0 {
			return nil, fmt.Errorf(
				"xlsx source %d does not contain usable headers",
				sourceIndex,
			)
		}

		workbooks = append(
			workbooks,
			workbook,
		)
		workbookColumns = append(
			workbookColumns,
			descriptors,
		)
		for _, descriptor := range descriptors {
			if descriptor.Key == "__eif_stt" {
				continue
			}
			if _, ok := seen[descriptor.Key]; ok {
				continue
			}
			seen[descriptor.Key] = struct{}{}
			columns = append(
				columns,
				mergeUnionColumn{
					Key:    descriptor.Key,
					Header: descriptor.Header,
				},
			)
		}
	}

	rows := make(
		[][]any,
		0,
	)
	sequence := 0
	for sourceIndex, workbook := range workbooks {
		sourceName := strings.TrimSpace(sources[sourceIndex].Name)
		if sourceName == "" {
			sourceName = fmt.Sprintf(
				"Nguồn %d",
				sourceIndex+1,
			)
		}

		byColumn := make(
			map[string]mergeWorkbookColumn,
			len(workbookColumns[sourceIndex]),
		)
		for _, descriptor := range workbookColumns[sourceIndex] {
			byColumn[descriptor.Column] = descriptor
		}

		for rowIndex := workbook.HeaderIndex + 1; rowIndex < len(workbook.Rows); rowIndex++ {
			sourceRow := workbook.Rows[rowIndex]
			if !isMergeRowPopulated(
				sourceRow,
				workbook.SharedStrings,
			) {
				continue
			}

			values := make(
				map[string]string,
				len(sourceRow.Cells),
			)
			hasBusinessValue := false
			for _, cell := range sourceRow.Cells {
				descriptor, ok := byColumn[getMergeCellColumn(cell.Ref)]
				if !ok || descriptor.Key == "__eif_stt" {
					continue
				}
				value := getMergeCellText(
					cell,
					workbook.SharedStrings,
				)
				values[descriptor.Key] = value
				if strings.TrimSpace(value) != "" {
					hasBusinessValue = true
				}
			}

			// Bỏ các dòng rỗng/chỉ có STT. Existing Merge cũng chỉ giữ dòng có
			// dữ liệu; fallback này tránh tạo dòng trắng khi template có footer.
			if !hasBusinessValue {
				continue
			}

			sequence++
			output := make(
				[]any,
				len(columns),
			)
			output[0] = sequence
			output[1] = sourceName
			for columnIndex := 2; columnIndex < len(columns); columnIndex++ {
				output[columnIndex] = values[columns[columnIndex].Key]
			}
			rows = append(
				rows,
				output,
			)
		}
	}

	tableColumns := make(
		[]Column,
		0,
		len(columns),
	)
	for index, column := range columns {
		width := float64(utf8.RuneCountInString(column.Header) + 4)
		if width < 12 {
			width = 12
		}
		if width > 42 {
			width = 42
		}
		isNumber := false
		if index == 0 {
			width = 8
			isNumber = true
		} else if index == 1 {
			width = 20
		}
		tableColumns = append(
			tableColumns,
			Column{
				Header:   column.Header,
				Width:    width,
				IsNumber: isNumber,
			},
		)
	}

	if strings.TrimSpace(title) == "" {
		title = "Hóa đơn điện tử"
	}

	return BuildTable(
		Table{
			Title:   title,
			Sheet:   "Hóa đơn điện tử",
			Columns: tableColumns,
			Rows:    rows,
		},
	)
}

func mergeWorkbookHeaderColumns(
	workbook *mergeWorkbook,
	sourceIndex int,
) []mergeWorkbookColumn {
	if workbook == nil {
		return nil
	}

	columns := make(
		[]string,
		0,
		len(workbook.Header),
	)
	for column := range workbook.Header {
		columns = append(
			columns,
			column,
		)
	}
	sort.SliceStable(
		columns,
		func(i, j int) bool {
			return getMergeColumnNumber(columns[i]) < getMergeColumnNumber(columns[j])
		},
	)

	occurrences := make(map[string]int)
	result := make(
		[]mergeWorkbookColumn,
		0,
		len(columns),
	)
	for _, column := range columns {
		header := strings.TrimSpace(workbook.Header[column])
		if strings.EqualFold(
			header,
			"STT",
		) {
			result = append(
				result,
				mergeWorkbookColumn{
					Column: column,
					Key:    "__eif_stt",
					Header: "STT",
				},
			)
			continue
		}

		// Header rỗng không thể map an toàn giữa Standard và SCO. Nếu giữ lại,
		// tách theo source + column để tuyệt đối không đẩy dữ liệu sang nhầm cột.
		if header == "" {
			result = append(
				result,
				mergeWorkbookColumn{
					Column: column,
					Key: fmt.Sprintf(
						"__blank_%d_%s",
						sourceIndex,
						column,
					),
					Header: fmt.Sprintf(
						"Cột %s",
						column,
					),
				},
			)
			continue
		}

		base := normalizeMergeHeaderKey(header)
		occurrences[base]++
		key := fmt.Sprintf(
			"%s#%d",
			base,
			occurrences[base],
		)
		result = append(
			result,
			mergeWorkbookColumn{
				Column: column,
				Key:    key,
				Header: header,
			},
		)
	}
	return result
}

func normalizeMergeHeaderKey(value string) string {
	return strings.ToLower(
		strings.Join(
			strings.Fields(value),
			" ",
		),
	)
}

// Validate kiểm tra payload có phải workbook XLSX mà EIF có thể đọc/merge hay không.
// Hàm không ghi nội dung file ra log để tránh rò dữ liệu hóa đơn.
func Validate(body []byte) error {
	_, err := readMergeWorkbook(body)
	return err
}

// Merge nhận nhiều file làm đầu vào -> merge lại -> trả ra thành 1 file duy nhất
func Merge(files [][]byte) (
	[]byte,
	error,
) {
	if len(files) == 0 {
		return nil, fmt.Errorf("xlsx files are required")
	}

	// Đọc tất cả workbook
	workbooks := make(
		[]*mergeWorkbook,
		0,
		len(files),
	)
	for index, body := range files {
		workbook, err := readMergeWorkbook(body)
		if err != nil {
			return nil, fmt.Errorf(
				"read xlsx file %d: %w",
				index,
				err,
			)
		}
		workbooks = append(
			workbooks,
			workbook,
		)
	}

	if len(workbooks) == 1 {
		return append(
			[]byte(nil),
			workbooks[0].Body...,
		), nil
	}

	first := workbooks[0]
	for index := 1; index < len(workbooks); index++ {
		if err := validateMergeHeader(
			first,
			workbooks[index],
		); err != nil {
			return nil, fmt.Errorf(
				"xlsx file %d: %w",
				index,
				err,
			)
		}
	}

	rows := make(
		[]mergeRow,
		0,
	)
	// Riêng workbook đầu tiên thì title + header đã được copy trước:
	for index := 0; index <= first.HeaderIndex; index++ {
		rows = append(
			rows,
			normalizeMergeRow(
				first.Rows[index],
				first.SharedStrings,
				first.Rows[index].R,
			),
		)
	}

	/*
		- Tức là mỗi workbook:

		rows trước header
		header
		-----------------
		data row 1
		data row 2
		data row 3

		- output:

		Title              ← từ workbook đầu tiên
		Header             ← từ workbook đầu tiên

		February rows
		March rows
		April rows
		...
	*/
	nextRow := first.HeaderRow + 1
	sequence := 0
	for _, workbook := range workbooks {
		for index := workbook.HeaderIndex + 1; index < len(workbook.Rows); index++ {
			source := workbook.Rows[index]
			if !isMergeRowPopulated(
				source,
				workbook.SharedStrings,
			) {
				continue
			}

			row := normalizeMergeRow(
				source,
				workbook.SharedStrings,
				nextRow,
			)

			// đánh lại STT
			if isMergeColumnPresent(
				row,
				first.STTColumn,
			) {
				sequence++
				setMergeSequence(
					&row,
					first.STTColumn,
					sequence,
				)
			}

			rows = append(
				rows,
				row,
			)
			nextRow++
		}
	}

	// Go structs -> chuyển lại thành XML.
	sheet, err := replaceMergeSheetRows(
		first.Sheet,
		rows,
		nextRow-1,
	)
	if err != nil {
		return nil, err
	}

	// Chuyển từ XML -> XLSX
	return buildMergedWorkbook(
		first.Body,
		sheet,
	)
}

// Mở XLSX
func readMergeWorkbook(body []byte) (
	*mergeWorkbook,
	error,
) {
	if len(body) == 0 {
		return nil, fmt.Errorf("xlsx file is empty")
	}

	// mở giống như zip
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

	//invoice.xlsx
	//│
	//├── [Content_Types].xml
	//├── xl/
	//│   ├── workbook.xml
	//│   ├── styles.xml
	//│   ├── sharedStrings.xml       ← text
	//│   │
	//│   └── worksheets/
	//│       └── sheet1.xml          ← cells/rows
	var sheet []byte
	var sharedStringsXML []byte
	for _, file := range reader.File {
		switch file.Name {
		case worksheetPath:
			sheet, err = readMergeZipEntry(file)
			if err != nil {
				return nil, err
			}
		case sharedStringsPath:
			sharedStringsXML, err = readMergeZipEntry(file)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(sheet) == 0 {
		return nil, fmt.Errorf(
			"%s is missing",
			worksheetPath,
		)
	}

	sharedStrings, err := parseMergeSharedStrings(sharedStringsXML)
	if err != nil {
		return nil, err
	}

	var worksheet mergeWorksheet
	if err := xml.Unmarshal(
		sheet,
		&worksheet,
	); err != nil {
		return nil, fmt.Errorf(
			"decode worksheet: %w",
			err,
		)
	}
	if len(worksheet.SheetData.Rows) == 0 {
		return nil, fmt.Errorf("worksheet does not contain rows")
	}

	headerIndex, headerRow, sttColumn, header, err := getMergeHeader(
		worksheet.SheetData.Rows,
		sharedStrings,
	)
	if err != nil {
		return nil, err
	}

	return &mergeWorkbook{
		Body:          body,
		Sheet:         sheet,
		Rows:          worksheet.SheetData.Rows,
		SharedStrings: sharedStrings,
		HeaderIndex:   headerIndex,
		HeaderRow:     headerRow,
		STTColumn:     sttColumn,
		Header:        header,
	}, nil
}

func readMergeZipEntry(file *zip.File) (
	[]byte,
	error,
) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return data, nil
}

/*
<sst>

	<si>
	    <t>STT</t>
	</si>

	<si>
	    <t>Mã số thuế</t>
	</si>

</sst>

------> Chuyển thành:

	[]string{
		"STT",
		"Mã số thuế",
		...
	}
*/
func parseMergeSharedStrings(data []byte) (
	[]string,
	error,
) {
	if len(data) == 0 {
		return nil, nil
	}

	var shared mergeSharedStrings
	if err := xml.Unmarshal(
		data,
		&shared,
	); err != nil {
		return nil, fmt.Errorf(
			"decode shared strings: %w",
			err,
		)
	}

	result := make(
		[]string,
		0,
		len(shared.Items),
	)
	for _, item := range shared.Items {
		result = append(
			result,
			getMergeRichText(
				item.Text,
				item.Runs,
			),
		)
	}
	return result, nil
}

// Tìm row nào chứa STT
func getMergeHeader(
	rows []mergeRow,
	sharedStrings []string,
) (
	int,
	int,
	string,
	map[string]string,
	error,
) {
	for index, row := range rows {
		header := make(map[string]string)
		sttColumn := ""

		for _, cell := range row.Cells {
			column := getMergeCellColumn(cell.Ref)
			if column == "" {
				continue
			}

			text := strings.TrimSpace(
				getMergeCellText(
					cell,
					sharedStrings,
				),
			)
			header[column] = text
			if strings.EqualFold(
				text,
				"STT",
			) {
				sttColumn = column
			}
		}

		if sttColumn != "" {
			return index, row.R, sttColumn, header, nil
		}
	}

	return 0, 0, "", nil, fmt.Errorf("worksheet header containing STT was not found")
}

// check header để không bị lệch cột
func validateMergeHeader(first, current *mergeWorkbook) error {
	if first.STTColumn != current.STTColumn {
		return fmt.Errorf(
			"worksheet STT column does not match: expected %s, got %s",
			first.STTColumn,
			current.STTColumn,
		)
	}

	if len(first.Header) != len(current.Header) {
		return fmt.Errorf("worksheet columns do not match")
	}

	for column, value := range first.Header {
		if currentValue, ok := current.Header[column]; !ok ||
			strings.TrimSpace(currentValue) != strings.TrimSpace(value) {
			return fmt.Errorf(
				"worksheet header does not match at column %s",
				column,
			)
		}
	}

	return nil
}

func normalizeMergeRow(
	source mergeRow,
	sharedStrings []string,
	rowNumber int,
) mergeRow {
	row := source
	// sửa cả row number
	row.R = rowNumber
	row.Cells = make(
		[]mergeCell,
		0,
		len(source.Cells),
	)

	for _, sourceCell := range source.Cells {
		cell := sourceCell
		column := getMergeCellColumn(sourceCell.Ref)
		if column == "" {
			continue
		}
		cell.Ref = column + strconv.Itoa(rowNumber)

		// chìa khóa của toàn bộ merge.
		/*
			- Thay vì giữ:

			<c t="s">
				<v>1</v>
			</c>

			+) code giải mã:

			sharedStrings[1]
			→ "Công ty XYZ"

			+) rồi đổi cell thành:

			<c t="inlineStr">
				<is>
					<t>Công ty XYZ</t>
				</is>
			</c>

			-> cell không còn phụ thuộc vào sharedStrings.xml nữa.
		*/
		if sourceCell.Type == "s" {
			cell.Type = "inlineStr"
			cell.Value = ""
			cell.Inline = mergeInlineString{
				Text: getMergeCellText(
					sourceCell,
					sharedStrings,
				),
			}
		}

		row.Cells = append(
			row.Cells,
			cell,
		)
	}

	return row
}

func isMergeRowPopulated(
	row mergeRow,
	sharedStrings []string,
) bool {
	for _, cell := range row.Cells {
		if strings.TrimSpace(
			getMergeCellText(
				cell,
				sharedStrings,
			),
		) != "" {
			return true
		}
		if strings.TrimSpace(cell.Formula) != "" {
			return true
		}
	}
	return false
}

func isMergeColumnPresent(
	row mergeRow,
	column string,
) bool {
	for _, cell := range row.Cells {
		if getMergeCellColumn(cell.Ref) == column {
			return true
		}
	}
	return false
}

// tìm cell nằm tại cột STT rồi đổi thành numeric cell:
func setMergeSequence(
	row *mergeRow,
	column string,
	sequence int,
) {
	for index := range row.Cells {
		if getMergeCellColumn(row.Cells[index].Ref) != column {
			continue
		}

		row.Cells[index].Type = ""
		row.Cells[index].Formula = ""
		row.Cells[index].Inline = mergeInlineString{}
		row.Cells[index].Value = strconv.Itoa(sequence)
		return
	}
}

// Giải mã cell
func getMergeCellText(
	cell mergeCell,
	sharedStrings []string,
) string {
	switch cell.Type {
	// cell.Value = "12" -> sharedStrings[12]
	case "s":
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err == nil && index >= 0 &&
			index < len(sharedStrings) {
			return sharedStrings[index]
		}
		return cell.Value
	// text nằm ngay bên trong cell.
	case "inlineStr":
		return getMergeRichText(
			cell.Inline.Text,
			cell.Inline.Runs,
		)
	default:
		return cell.Value
	}
}

// rich text: bold,..
/*
<si>
    <r>
        <rPr>
            ...
        </rPr>
        <t>Công ty </t>
    </r>

    <r>
        <t>ABC</t>
    </r>
</si>
*/
// -> Nối thành 1 plain text
func getMergeRichText(
	text string,
	runs []mergeTextRun,
) string {
	if len(runs) == 0 {
		return text
	}

	var builder strings.Builder
	if text != "" {
		builder.WriteString(text)
	}

	for _, run := range runs {
		builder.WriteString(run.Text)
	}

	return builder.String()
}

// lấy chữ cột
func getMergeCellColumn(ref string) string {
	ref = strings.TrimSpace(ref)
	index := 0
	for index < len(ref) {
		ch := ref[index]
		if ch < 'A' || ch > 'Z' {
			break
		}
		index++
	}
	return ref[:index]
}

func replaceMergeSheetRows(
	sheet []byte,
	rows []mergeRow,
	lastRow int,
) (
	[]byte,
	error,
) {
	text := string(sheet)
	sheetDataStart := strings.Index(
		text,
		"<sheetData",
	)
	if sheetDataStart < 0 {
		return nil, fmt.Errorf("worksheet sheetData is missing")
	}

	openEndRelative := strings.Index(
		text[sheetDataStart:],
		">",
	)
	if openEndRelative < 0 {
		return nil, fmt.Errorf("worksheet sheetData opening tag is invalid")
	}
	openEnd := sheetDataStart + openEndRelative + 1

	closeStart := strings.Index(
		text[openEnd:],
		"</sheetData>",
	)
	if closeStart < 0 {
		return nil, fmt.Errorf("worksheet sheetData closing tag is missing")
	}
	closeStart += openEnd

	var rowXML strings.Builder
	for _, row := range rows {
		rowXML.WriteString(marshalMergeRow(row))
	}

	text = text[:openEnd] + rowXML.String() + text[closeStart:]

	// Sheet có thể có: <dimension ref="A1:R100"/>
	// nghĩa là vùng worksheet đang sử dụng: A1 → R100
	// Sau merge có 5000 rows mà vẫn để: A1:R100
	// -> metadata sẽ không chính xác.
	text = updateMergeSheetRange(
		text,
		"dimension",
		lastRow,
	)

	// Header đang có filter: <autoFilter ref="A3:R100"/>
	// Sau merge tới row 5000: A3:R5000
	// nên code update phải cả
	text = updateMergeSheetRange(
		text,
		"autoFilter",
		lastRow,
	)

	return []byte(text), nil
}

// tạo XML row
/*
	mergeRow{
		R: 10,
		Cells: ...,
	}

	được biến thành:

	<row r="10">
		...
	</row>
*/
func marshalMergeRow(row mergeRow) string {
	var builder strings.Builder
	builder.WriteString(`<row r="`)
	builder.WriteString(strconv.Itoa(row.R))
	builder.WriteString(`"`)
	writeMergeAttribute(
		&builder,
		"ht",
		row.Height,
	)
	writeMergeAttribute(
		&builder,
		"customHeight",
		row.CustomHeight,
	)
	writeMergeAttribute(
		&builder,
		"s",
		row.Style,
	)
	writeMergeAttribute(
		&builder,
		"customFormat",
		row.CustomFormat,
	)
	writeMergeAttribute(
		&builder,
		"hidden",
		row.Hidden,
	)
	writeMergeAttribute(
		&builder,
		"outlineLevel",
		row.OutlineLevel,
	)
	builder.WriteString(`>`)

	cells := append(
		[]mergeCell(nil),
		row.Cells...,
	)
	// sort cell theo thứ tự cột đảm bảo:
	// A
	// B
	// C
	// D
	// ...
	// Z
	// AA
	// AB
	sort.SliceStable(
		cells,
		func(i, j int) bool {
			return getMergeColumnNumber(getMergeCellColumn(cells[i].Ref)) <
				getMergeColumnNumber(getMergeCellColumn(cells[j].Ref))
		},
	)

	for _, cell := range cells {
		builder.WriteString(marshalMergeCell(cell))
	}
	builder.WriteString(`</row>`)
	return builder.String()
}

/* marshalMergeCell tạo XML cho cell
- Ví dụ numeric:

mergeCell{
    Ref:   "A10",
    Value: "5",
}

->

<c r="A10">
    <v>5</v>
</c>

- Ví dụ inline string:

mergeCell{
    Ref:  "B10",
    Type: "inlineStr",
    Inline: {
        Text: "Công ty ABC",
    },
}

<c r="B10" t="inlineStr">
    <is>
        <t xml:space="preserve">
            Công ty ABC
        </t>
    </is>
</c>
*/

func marshalMergeCell(cell mergeCell) string {
	var builder strings.Builder
	builder.WriteString(`<c r="`)
	builder.WriteString(escapeAttribute(cell.Ref))
	builder.WriteString(`"`)
	writeMergeAttribute(
		&builder,
		"s",
		cell.Style,
	)
	writeMergeAttribute(
		&builder,
		"t",
		cell.Type,
	)
	builder.WriteString(`>`)

	if cell.Formula != "" {
		builder.WriteString(`<f>`)
		builder.WriteString(escapeText(cell.Formula))
		builder.WriteString(`</f>`)
	}

	if cell.Type == "inlineStr" {
		builder.WriteString(`<is><t xml:space="preserve">`)
		builder.WriteString(
			escapeText(
				getMergeRichText(
					cell.Inline.Text,
					cell.Inline.Runs,
				),
			),
		)
		builder.WriteString(`</t></is>`)
	} else if cell.Value != "" || cell.Formula != "" {
		builder.WriteString(`<v>`)
		builder.WriteString(escapeText(cell.Value))
		builder.WriteString(`</v>`)
	}

	builder.WriteString(`</c>`)
	return builder.String()
}

func writeMergeAttribute(
	builder *strings.Builder,
	key, value string,
) {
	if value == "" {
		return
	}
	builder.WriteString(` `)
	builder.WriteString(key)
	builder.WriteString(`="`)
	builder.WriteString(escapeAttribute(value))
	builder.WriteString(`"`)
}

// convert:
//
// A  → 1
// B  → 2
// ...
// Z  → 26
// AA → 27
// AB → 28
func getMergeColumnNumber(column string) int {
	result := 0
	for index := 0; index < len(column); index++ {
		result = result*26 + int(column[index]-'A'+1)
	}
	return result
}

func updateMergeSheetRange(
	sheet, tag string,
	lastRow int,
) string {
	expression := regexp.MustCompile(
		`(<` + regexp.QuoteMeta(tag) + `\b[^>]*\bref=")([A-Z]+)([0-9]+):([A-Z]+)([0-9]+)(")`,
	)

	return expression.ReplaceAllStringFunc(
		sheet,
		func(match string) string {
			parts := expression.FindStringSubmatch(match)
			if len(parts) != 7 {
				return match
			}
			return parts[1] + parts[2] + parts[3] + ":" + parts[4] + strconv.Itoa(lastRow) + parts[6]
		},
	)
}

// Mở first.xlsx -> copy từng file bên trong ->
// - nếu file != sheet1.xml -> copy nguyên ->
// - nếu file == sheet1.xml -> dùng sheet XML mới
// ZIP lại -> merged.xlsx
func buildMergedWorkbook(first, sheet []byte) (
	[]byte,
	error,
) {
	reader, err := zip.NewReader(
		bytes.NewReader(first),
		int64(len(first)),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open source xlsx: %w",
			err,
		)
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	for _, file := range reader.File {
		data, err := readMergeZipEntry(file)
		if err != nil {
			return nil, err
		}
		if file.Name == worksheetPath {
			data = sheet
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
	return buffer.Bytes(), nil
}

/* TỔNG KẾT LUỒNG
GDT
 │
 ├── February.xlsx
 ├── March.xlsx
 └── April.xlsx
          │
          ▼
      [][]byte
          │
          ▼
       Merge()
          │
          ▼
┌─────────────────────────────┐
│ readMergeWorkbook(file 1)   │
│ readMergeWorkbook(file 2)   │
│ readMergeWorkbook(file 3)   │
└─────────────────────────────┘
          │
          ▼
 mỗi XLSX được mở như ZIP
          │
          ▼
┌───────────────────────────────┐
│ sheet1.xml                    │
│ sharedStrings.xml             │
└───────────────────────────────┘
          │
          ▼
       XML Unmarshal
          │
          ▼
┌───────────────────────────────┐
│ []mergeRow                    │
│ []string sharedStrings        │
│ header                        │
│ STT column                    │
└───────────────────────────────┘
          │
          ▼
    validate headers
          │
          ▼
 lấy title/header file đầu
          │
          ▼
 append data của tất cả file
          │
          ▼
 shared string → inline string
          │
          ▼
 sửa row:
 A4 → A100
 B4 → B100
 ...
          │
          ▼
 đánh lại STT 1...N
          │
          ▼
 tạo <row> XML mới
          │
          ▼
 thay <sheetData>
          │
          ▼
 update dimension / autoFilter
          │
          ▼
 lấy first.xlsx làm template
          │
          ▼
 thay sheet1.xml
          │
          ▼
 ZIP lại
          │
          ▼
      merged.xlsx
*/
