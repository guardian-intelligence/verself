// Package wirecheck enforces the apiwire numeric-safety contract on a
// rendered OpenAPI 3.1 spec: integer int64/uint64 fields must either
// declare a JS-safe maximum, opt into bigint via x-js-wire, or use one
// of the apiwire decimal string DTOs. Designed to run as a Bazel test
// fed by a verself_openapi_yaml output, so spec drift fails CI before
// reaching a customer.
package wirecheck

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

const maxSafeInteger = 9007199254740991

type Violation struct {
	File string
	Line int
	Path string
	Msg  string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", v.File, v.Line, v.Path, v.Msg)
}

// CheckFile parses the OpenAPI 3.1 YAML at path and returns every wire
// violation it finds. A non-nil error indicates the file could not be
// read or parsed.
func CheckFile(path string) ([]Violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, errors.New("empty YAML document")
	}
	var out []Violation
	walk(path, root.Content[0], "$", &out)
	return out, nil
}

func walk(file string, node *yaml.Node, path string, out *[]Violation) {
	if node == nil {
		return
	}
	if node.Kind == yaml.MappingNode {
		checkSchemaNode(file, node, path, out)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			value := node.Content[i+1]
			walk(file, value, joinPath(path, key.Value), out)
		}
		return
	}
	if node.Kind == yaml.SequenceNode {
		for i, value := range node.Content {
			walk(file, value, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

func checkSchemaNode(file string, node *yaml.Node, path string, out *[]Violation) {
	format := scalarValue(mapValue(node, "format"))
	if format != "int64" && format != "uint64" {
		return
	}
	if !schemaTypeIncludesInteger(mapValue(node, "type")) {
		return
	}
	if scalarValue(mapValue(node, "x-js-wire")) == "bigint" {
		return
	}
	if maximum, ok := integerMaximum(node); ok && maximum <= maxSafeInteger {
		return
	}
	*out = append(*out, Violation{
		File: file,
		Line: node.Line,
		Path: path,
		Msg:  "integer int64/uint64 must have maximum <= Number.MAX_SAFE_INTEGER, x-js-wire: bigint, or use apiwire decimal string DTOs",
	})
}

func integerMaximum(node *yaml.Node) (uint64, bool) {
	maxNode := mapValue(node, "maximum")
	if maxNode == nil || maxNode.Kind != yaml.ScalarNode {
		return 0, false
	}
	if value, err := strconv.ParseUint(maxNode.Value, 10, 64); err == nil {
		return value, true
	}
	// Fall back to float for scientific/decimal notation (e.g. 1e6). Bound at
	// maxSafeInteger so the cast stays inside float64's exact-integer range;
	// any larger value would violate the wire contract anyway.
	value, err := strconv.ParseFloat(maxNode.Value, 64)
	if err != nil || value < 0 || value > maxSafeInteger || value != math.Trunc(value) {
		return 0, false
	}
	return uint64(value), true
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func schemaTypeIncludesInteger(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value == "integer"
	case yaml.SequenceNode:
		for _, value := range node.Content {
			if scalarValue(value) == "integer" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func joinPath(path, key string) string {
	if path == "$" {
		return "$." + key
	}
	return path + "." + key
}
