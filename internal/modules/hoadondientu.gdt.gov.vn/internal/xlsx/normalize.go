package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var cellTypeAttributePattern = regexp.MustCompile(`(?i)[ \t\r\n]+t[ \t\r\n]*=[ \t\r\n]*(?:"[^"]*"|'[^']*')`)

func NormalizeNumericDisplay(body []byte) (
	[]byte,
	error,
) {
	if len(body) == 0 {
		return nil, fmt.Errorf("xlsx file is empty")
	}

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

	var sheet []byte
	for _, file := range reader.File {
		if file.Name != worksheetPath {
			continue
		}
		sheet, err = readMergeZipEntry(file)
		if err != nil {
			return nil, err
		}
		break
	}
	if len(sheet) == 0 {
		return nil, fmt.Errorf(
			"%s is missing",
			worksheetPath,
		)
	}

	normalized, changed, err := normalizeWorksheetNumericDisplay(sheet)
	if err != nil {
		return nil, err
	}
	if !changed {
		return append(
			[]byte(nil),
			body...,
		), nil
	}

	return buildMergedWorkbook(
		body,
		normalized,
	)
}

func normalizeWorksheetNumericDisplay(sheet []byte) (
	[]byte,
	bool,
	error,
) {
	if err := validateWorksheetXML(sheet); err != nil {
		return nil, false, fmt.Errorf(
			"decode worksheet: %w",
			err,
		)
	}

	text := string(sheet)
	var out strings.Builder
	out.Grow(len(text) + 256)

	cursor := 0
	changed := false
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
			return nil, false, fmt.Errorf("worksheet cell opening tag is invalid")
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
			return nil, false, fmt.Errorf("worksheet cell closing tag is missing")
		}
		closeStart := openEnd + 1 + closeStartRelative
		cellEnd := closeStart + len("</c>")
		inner := text[openEnd+1 : closeStart]

		value, eligible := getRawNumericCellValue(
			startTag,
			inner,
		)
		if !eligible {
			out.WriteString(text[cellStart:cellEnd])
			cursor = cellEnd
			continue
		}

		normalized, protectAsText := normalizeNumericLiteral(value)
		if !protectAsText {
			out.WriteString(text[cellStart:cellEnd])
			cursor = cellEnd
			continue
		}

		changed = true
		out.WriteString(
			setRawCellType(
				startTag,
				"inlineStr",
			),
		)
		out.WriteString(`<is><t xml:space="preserve">`)
		out.WriteString(escapeText(normalized))
		out.WriteString(`</t></is></c>`)
		cursor = cellEnd
	}

	result := []byte(out.String())
	if changed {
		if err := validateWorksheetXML(result); err != nil {
			return nil, false, fmt.Errorf(
				"validate normalized worksheet: %w",
				err,
			)
		}
	}
	return result, changed, nil
}

func validateWorksheetXML(sheet []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(sheet))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func findNextCellStart(
	text string,
	from int,
) int {
	if from < 0 {
		from = 0
	}
	for from < len(text) {
		relative := strings.Index(
			text[from:],
			"<c",
		)
		if relative < 0 {
			return -1
		}
		index := from + relative
		next := index + 2
		if next >= len(text) || isXMLTagBoundary(text[next]) {
			return index
		}
		from = next
	}
	return -1
}

func isXMLTagBoundary(ch byte) bool {
	switch ch {
	case ' ', '\t', '\r', '\n', '>', '/':
		return true
	default:
		return false
	}
}

func getRawNumericCellValue(startTag, inner string) (
	string,
	bool,
) {
	cellType := getRawCellType(startTag)
	if cellType != "" && !strings.EqualFold(
		cellType,
		"n",
	) {
		return "", false
	}

	if findElementStart(
		inner,
		"f",
	) >= 0 {
		return "", false
	}

	valueStart := findElementStart(
		inner,
		"v",
	)
	if valueStart < 0 {
		return "", false
	}
	openEndRelative := strings.IndexByte(
		inner[valueStart:],
		'>',
	)
	if openEndRelative < 0 {
		return "", false
	}
	openEnd := valueStart + openEndRelative
	closeStartRelative := strings.Index(
		inner[openEnd+1:],
		"</v>",
	)
	if closeStartRelative < 0 {
		return "", false
	}
	closeStart := openEnd + 1 + closeStartRelative

	value := strings.TrimSpace(inner[openEnd+1 : closeStart])
	if value == "" || strings.ContainsAny(
		value,
		"<>&",
	) {
		return "", false
	}
	return value, true
}

func findElementStart(text, name string) int {
	needle := "<" + name
	from := 0
	for from < len(text) {
		relative := strings.Index(
			text[from:],
			needle,
		)
		if relative < 0 {
			return -1
		}
		index := from + relative
		next := index + len(needle)
		if next >= len(text) || isXMLTagBoundary(text[next]) {
			return index
		}
		from = next
	}
	return -1
}

func getRawCellType(startTag string) string {
	match := cellTypeAttributePattern.FindString(startTag)
	if match == "" {
		return ""
	}

	equals := strings.IndexByte(
		match,
		'=',
	)
	if equals < 0 {
		return ""
	}
	value := strings.TrimSpace(match[equals+1:])
	if len(value) < 2 {
		return ""
	}
	quote := value[0]
	if (quote != '\'' && quote != '"') || value[len(value)-1] != quote {
		return ""
	}
	return value[1 : len(value)-1]
}

func setRawCellType(startTag, cellType string) string {
	if location := cellTypeAttributePattern.FindStringIndex(startTag); location != nil {
		return startTag[:location[0]] + ` t="` + escapeAttribute(cellType) + `"` + startTag[location[1]:]
	}

	end := strings.LastIndexByte(
		startTag,
		'>',
	)
	if end < 0 {
		return startTag
	}
	return startTag[:end] + ` t="` + escapeAttribute(cellType) + `"` + startTag[end:]
}

func normalizeNumericLiteral(value string) (
	string,
	bool,
) {
	value = strings.TrimSpace(value)
	if value == "" {
		return value, false
	}

	if strings.ContainsAny(
		value,
		"eE",
	) {
		expanded, ok := expandScientificNotation(value)
		if ok {
			return expanded, true
		}
	}

	if countSignificantDecimalDigits(value) > 15 {
		return value, true
	}
	return value, false
}

func countSignificantDecimalDigits(value string) int {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(
		value,
		"+",
	)
	value = strings.TrimPrefix(
		value,
		"-",
	)
	if value == "" {
		return 0
	}

	count := 0
	seenNonZero := false
	for _, r := range value {
		if r == '.' {
			continue
		}
		if !unicode.IsDigit(r) {
			return 0
		}
		if r != '0' {
			seenNonZero = true
		}
		if seenNonZero {
			count++
		}
	}
	return count
}

func expandScientificNotation(value string) (
	string,
	bool,
) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	negative := false
	if value[0] == '+' || value[0] == '-' {
		negative = value[0] == '-'
		value = value[1:]
	}

	parts := strings.FieldsFunc(
		value,
		func(r rune) bool { return r == 'e' || r == 'E' },
	)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	exponent, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", false
	}

	mantissa := parts[0]
	if strings.Count(
		mantissa,
		".",
	) > 1 {
		return "", false
	}
	dot := strings.IndexByte(
		mantissa,
		'.',
	)
	if dot < 0 {
		dot = len(mantissa)
	}
	digits := strings.ReplaceAll(
		mantissa,
		".",
		"",
	)
	if digits == "" {
		return "", false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", false
		}
	}

	decimalPosition := dot + exponent
	var result string
	switch {
	case decimalPosition <= 0:
		result = "0." + strings.Repeat(
			"0",
			-decimalPosition,
		) + digits
	case decimalPosition >= len(digits):
		result = digits + strings.Repeat(
			"0",
			decimalPosition-len(digits),
		)
	default:
		result = digits[:decimalPosition] + "." + digits[decimalPosition:]
	}

	if negative && result != "0" {
		result = "-" + result
	}
	return result, true
}
