package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxSafeInteger = 9007199254740991

type violation struct {
	file string
	line int
	path string
	msg  string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: openapi-wire-check <openapi-3.1.yaml>...")
		os.Exit(2)
	}

	var violations []violation
	for _, path := range os.Args[1:] {
		found, err := checkFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(2)
		}
		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return
	}
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "%s:%d: %s: %s\n", v.file, v.line, v.path, v.msg)
	}
	os.Exit(1)
}

func checkFile(path string) ([]violation, error) {
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
	var out []violation
	walk(path, root.Content[0], "$", &out)
	return out, nil
}

func walk(file string, node *yaml.Node, path string, out *[]violation) {
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

func checkSchemaNode(file string, node *yaml.Node, path string, out *[]violation) {
	format := scalarValue(mapValue(node, "format"))
	if format != "int64" && format != "uint64" {
		return
	}
	if scalarValue(mapValue(node, "type")) != "integer" {
		return
	}
	if scalarValue(mapValue(node, "x-js-wire")) == "bigint" {
		return
	}
	if maximum, ok := integerMaximum(node); ok && maximum <= maxSafeInteger {
		return
	}
	*out = append(*out, violation{
		file: file,
		line: node.Line,
		path: path,
		msg:  "integer int64/uint64 must have maximum <= Number.MAX_SAFE_INTEGER, x-js-wire: bigint, or use apiwire decimal string DTOs",
	})
}

func integerMaximum(node *yaml.Node) (uint64, bool) {
	maxNode := mapValue(node, "maximum")
	if maxNode == nil || maxNode.Kind != yaml.ScalarNode {
		return 0, false
	}
	if strings.ContainsAny(maxNode.Value, ".eE") {
		value, err := strconv.ParseFloat(maxNode.Value, 64)
		if err != nil || value < 0 || value != float64(uint64(value)) {
			return 0, false
		}
		return uint64(value), true
	}
	value, err := strconv.ParseUint(maxNode.Value, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
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

func joinPath(path, key string) string {
	if path == "$" {
		return "$." + key
	}
	return path + "." + key
}
