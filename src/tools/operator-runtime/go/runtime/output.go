package runtime

import (
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

type Table struct {
	Headers []string
	Rows    [][]string
}

func PrintTable(w io.Writer, table Table) error {
	widths := make([]int, len(table.Headers))
	for i, header := range table.Headers {
		widths[i] = len(header)
	}
	for _, row := range table.Rows {
		for i, value := range row {
			if i < len(widths) && len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}
	printTableRow(w, table.Headers, widths)
	separators := make([]string, len(widths))
	for i, width := range widths {
		separators[i] = strings.Repeat("-", width)
	}
	printTableRow(w, separators, widths)
	for _, row := range table.Rows {
		printTableRow(w, row, widths)
	}
	_, err := fmt.Fprintf(w, "(%d rows)\n", len(table.Rows))
	return err
}

func printTableRow(w io.Writer, row []string, widths []int) {
	for i, value := range row {
		if i > 0 {
			fmt.Fprint(w, " | ")
		}
		width := 0
		if i < len(widths) {
			width = widths[i]
		}
		fmt.Fprintf(w, "%-*s", width, value)
	}
	fmt.Fprintln(w)
}

func FormatValue(value any) string {
	if value == nil {
		return "NULL"
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "NULL"
		}
		return FormatValue(rv.Elem().Interface())
	}
	switch v := value.(type) {
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}
		return `\x` + hex.EncodeToString(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}
