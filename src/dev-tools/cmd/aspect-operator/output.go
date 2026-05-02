package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	opruntime "github.com/verself/operator-runtime/runtime"
)

func printTableFormat(w io.Writer, table opruntime.Table, format string) error {
	switch normalizeOutputFormat(format) {
	case "json":
		return printTableJSON(w, table)
	case "csv":
		return printDelimited(w, table, ',')
	case "tsv":
		return printDelimited(w, table, '\t')
	default:
		return opruntime.PrintTable(w, table)
	}
}

func normalizeOutputFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table", "prettycompact", "prettycompactmonoblock":
		return "table"
	case "json", "jsoneachrow":
		return "json"
	case "csv", "csvwithnames":
		return "csv"
	case "tsv", "tsvraw", "tsvwithnames", "tabseparated", "tabseparatedwithnames":
		return "tsv"
	default:
		return format
	}
}

func printTableJSON(w io.Writer, table opruntime.Table) error {
	rows := make([]map[string]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		obj := make(map[string]string, len(table.Headers))
		for i, header := range table.Headers {
			if i < len(row) {
				obj[header] = row[i]
			} else {
				obj[header] = ""
			}
		}
		rows = append(rows, obj)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func printDelimited(w io.Writer, table opruntime.Table, comma rune) error {
	writer := csv.NewWriter(w)
	writer.Comma = comma
	if err := writer.Write(table.Headers); err != nil {
		return err
	}
	for _, row := range table.Rows {
		record := make([]string, len(table.Headers))
		for i := range table.Headers {
			if i < len(row) {
				record[i] = row[i]
			}
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("write delimited output: %w", err)
	}
	return nil
}
