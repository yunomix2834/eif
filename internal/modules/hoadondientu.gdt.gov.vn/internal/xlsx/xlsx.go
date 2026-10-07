package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

const contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type Column struct {
	Header   string
	Width    float64
	IsNumber bool
}

type Table struct {
	Title   string
	Sheet   string
	Columns []Column
	Rows    [][]any
}

func GetContentType() string {
	return contentType
}

func BuildTable(table Table) (
	[]byte,
	error,
) {
	if strings.TrimSpace(table.Sheet) == "" {
		table.Sheet = "Hoá đơn điện tử"
	}
	if len(table.Columns) == 0 {
		return nil, fmt.Errorf("xlsx columns are required")
	}

	var out bytes.Buffer
	zw := zip.NewWriter(&out)

	files := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", buildContentTypesXML()},
		{"_rels/.rels", buildRootRelationshipsXML()},
		{"docProps/app.xml", buildAppXML()},
		{"docProps/core.xml", buildCoreXML()},
		{"xl/workbook.xml", buildWorkbookXML(table.Sheet)},
		{"xl/_rels/workbook.xml.rels", buildWorkbookRelationshipsXML()},
		{"xl/styles.xml", buildStylesXML()},
		{"xl/worksheets/sheet1.xml", buildWorksheetXML(table)},
	}

	for _, file := range files {
		writer, err := zw.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(file.body)); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

func buildContentTypesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
		`</Types>`
}

func buildRootRelationshipsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
		`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
		`</Relationships>`
}

func buildAppXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
		`<Application>EIF</Application>` +
		`</Properties>`
}

func buildCoreXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:creator>EIF</dc:creator>` +
		`<cp:lastModifiedBy>EIF</cp:lastModifiedBy>` +
		`</cp:coreProperties>`
}

func buildWorkbookXML(sheet string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
		`<sheets><sheet name="` + escapeAttribute(sheet) + `" sheetId="1" r:id="rId1"/></sheets>` +
		`</workbook>`
}

func buildWorkbookRelationshipsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`</Relationships>`
}

func buildStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0.########"/></numFmts>` +
		`<fonts count="3">` +
		`<font><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
		`<font><b/><sz val="14"/><name val="Calibri"/><family val="2"/></font>` +
		`<font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font>` +
		`</fonts>` +
		`<fills count="3">` +
		`<fill><patternFill patternType="none"/></fill>` +
		`<fill><patternFill patternType="gray125"/></fill>` +
		`<fill><patternFill patternType="solid"><fgColor rgb="FFE7E6E6"/><bgColor indexed="64"/></patternFill></fill>` +
		`</fills>` +
		`<borders count="2">` +
		`<border><left/><right/><top/><bottom/><diagonal/></border>` +
		`<border><left style="thin"><color rgb="FFD9D9D9"/></left><right style="thin"><color rgb="FFD9D9D9"/></right><top style="thin"><color rgb="FFD9D9D9"/></top><bottom style="thin"><color rgb="FFD9D9D9"/></bottom><diagonal/></border>` +
		`</borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		`<cellXfs count="5">` +
		`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
		`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>` +
		`<xf numFmtId="0" fontId="2" fillId="2" borderId="1" xfId="0" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>` +
		`<xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` +
		`<xf numFmtId="164" fontId="0" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment vertical="top"/></xf>` +
		`</cellXfs>` +
		`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
		`</styleSheet>`
}

func buildWorksheetXML(table Table) string {
	lastColumn := buildColumnName(len(table.Columns))
	lastRow := len(table.Rows) + 2
	if lastRow < 2 {
		lastRow = 2
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="2" topLeftCell="A3" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	b.WriteString(`<cols>`)
	for i, column := range table.Columns {
		width := column.Width
		if width <= 0 {
			width = 15
		}
		index := i + 1
		b.WriteString(`<col min="`)
		b.WriteString(strconv.Itoa(index))
		b.WriteString(`" max="`)
		b.WriteString(strconv.Itoa(index))
		b.WriteString(`" width="`)
		b.WriteString(
			strconv.FormatFloat(
				width,
				'f',
				-1,
				64,
			),
		)
		b.WriteString(`" customWidth="1"/>`)
	}
	b.WriteString(`</cols>`)
	b.WriteString(`<sheetData>`)

	b.WriteString(`<row r="1" ht="24" customHeight="1">`)
	b.WriteString(
		buildInlineStringCell(
			"A1",
			table.Title,
			1,
		),
	)
	b.WriteString(`</row>`)

	b.WriteString(`<row r="2" ht="34" customHeight="1">`)
	for i, column := range table.Columns {
		b.WriteString(
			buildInlineStringCell(
				buildColumnName(i+1)+"2",
				column.Header,
				2,
			),
		)
	}
	b.WriteString(`</row>`)

	for rowIndex, row := range table.Rows {
		excelRow := rowIndex + 3
		b.WriteString(`<row r="`)
		b.WriteString(strconv.Itoa(excelRow))
		b.WriteString(`">`)
		for colIndex, column := range table.Columns {
			var value any
			if colIndex < len(row) {
				value = row[colIndex]
			}
			ref := buildColumnName(colIndex+1) + strconv.Itoa(excelRow)
			if column.IsNumber {
				b.WriteString(
					buildNumberCell(
						ref,
						value,
					),
				)
			} else {
				b.WriteString(
					buildValueCell(
						ref,
						value,
					),
				)
			}
		}
		b.WriteString(`</row>`)
	}

	b.WriteString(`</sheetData>`)

	b.WriteString(`<autoFilter ref="A2:`)
	b.WriteString(lastColumn)
	b.WriteString(strconv.Itoa(lastRow))
	b.WriteString(`"/>`)
	b.WriteString(`<mergeCells count="1"><mergeCell ref="A1:`)
	b.WriteString(lastColumn)
	b.WriteString(`1"/></mergeCells>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

func buildValueCell(
	ref string,
	value any,
) string {
	if value == nil {
		return `<c r="` + ref + `" s="3"/>`
	}

	switch typed := value.(type) {
	case string:
		return buildInlineStringCell(
			ref,
			typed,
			3,
		)
	case []byte:
		return buildInlineStringCell(
			ref,
			string(typed),
			3,
		)
	case bool:
		if typed {
			return `<c r="` + ref + `" s="3" t="b"><v>1</v></c>`
		}
		return `<c r="` + ref + `" s="3" t="b"><v>0</v></c>`
	default:
		return buildInlineStringCell(
			ref,
			fmt.Sprint(value),
			3,
		)
	}
}

func buildNumberCell(
	ref string,
	value any,
) string {
	if value == nil {
		return `<c r="` + ref + `" s="4"/>`
	}

	var text string
	switch typed := value.(type) {
	case int:
		text = strconv.Itoa(typed)
	case int64:
		text = strconv.FormatInt(
			typed,
			10,
		)
	case float64:
		text = strconv.FormatFloat(
			typed,
			'f',
			-1,
			64,
		)
	case float32:
		text = strconv.FormatFloat(
			float64(typed),
			'f',
			-1,
			32,
		)
	default:
		return buildValueCell(
			ref,
			value,
		)
	}

	return `<c r="` + ref + `" s="4"><v>` + text + `</v></c>`
}

func buildInlineStringCell(
	ref, value string,
	style int,
) string {
	return `<c r="` + ref + `" s="` + strconv.Itoa(style) + `" t="inlineStr"><is><t xml:space="preserve">` + escapeText(value) + `</t></is></c>`
}

func escapeText(value string) string {
	value = strings.Map(
		func(r rune) rune {
			if r == 0x9 || r == 0xA || r == 0xD ||
				r >= 0x20 && r <= 0xD7FF ||
				r >= 0xE000 && r <= 0xFFFD ||
				r >= 0x10000 && r <= 0x10FFFF {
				return r
			}
			return -1
		},
		value,
	)

	var b bytes.Buffer
	_ = xml.EscapeText(
		&b,
		[]byte(value),
	)
	return b.String()
}

func escapeAttribute(value string) string {
	return strings.NewReplacer(
		"&",
		"&amp;",
		`"`,
		"&quot;",
		"<",
		"&lt;",
		">",
		"&gt;",
	).Replace(value)
}

// Số -> chữ cái là tên cột:
// buildColumnName(28) -> cột "AB"
func buildColumnName(index int) string {
	if index <= 0 {
		return "A"
	}

	var result []byte
	for index > 0 {
		index--
		result = append(
			[]byte{
				byte('A' + index%26),
			},
			result...,
		)
		index /= 26
	}
	return string(result)
}
