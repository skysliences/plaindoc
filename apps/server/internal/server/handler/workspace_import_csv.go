package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

var csvDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Excel keeps only 15 significant digits. Keep identifiers, leading zeroes and
// formula-like strings literal; ordinary decimal quantities remain usable in sums.
func workspaceCSVCell(value string) any {
	digits := strings.TrimPrefix(value, "-")
	if len(digits) <= 16 && len(strings.ReplaceAll(digits, ".", "")) <= 15 && csvDecimal.MatchString(value) {
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			return n
		}
	}
	return value
}

func workspaceCSVWorkbook(ctx context.Context, raw []byte) ([]byte, error) {
	text, err := decodeWorkspaceReadableText(raw, true)
	if err != nil {
		return nil, err
	}
	// Bound the number of fields before encoding/csv allocates the record.
	fields, quoted := 1, false
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				fields++
				if fields > excelize.MaxColumns {
					return nil, fmt.Errorf("CSV 每行最多支持 %d 列", excelize.MaxColumns)
				}
			}
		case '\n':
			if !quoted {
				fields = 1
			}
		}
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1 // Ragged records map naturally to a worksheet.
	reader.ReuseRecord = true
	book := excelize.NewFile()
	defer book.Close()
	stream, err := book.NewStreamWriter(book.GetSheetName(0))
	if err != nil {
		return nil, err
	}
	lastPhysicalLine, row := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV 引号或分隔符格式错误：%w", err)
		}
		// FieldPos includes blank lines skipped by encoding/csv. Newlines inside
		// a quoted value belong to one cell and must not advance the worksheet.
		line, _ := reader.FieldPos(0)
		row += line - lastPhysicalLine
		lastPhysicalLine = line
		if row > excelize.TotalRows || len(record) > excelize.MaxColumns {
			return nil, fmt.Errorf("CSV 超出 Excel 工作表的行列上限（第 %d 行）", row)
		}
		cells := make([]any, len(record))
		for i, value := range record {
			if utf8.RuneCountInString(value) > excelize.TotalCellChars {
				return nil, fmt.Errorf("CSV 第 %d 行第 %d 列超过单元格长度上限", row, i+1)
			}
			lastPhysicalLine += strings.Count(value, "\n")
			cells[i] = workspaceCSVCell(value)
		}
		if err := stream.SetRow("A"+strconv.Itoa(row), cells); err != nil {
			return nil, err
		}
	}
	if err := stream.Flush(); err != nil {
		return nil, err
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
