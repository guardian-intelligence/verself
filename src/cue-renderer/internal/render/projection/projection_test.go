package projection

import (
	"reflect"
	"strings"
	"testing"
)

func TestMap(t *testing.T) {
	parent := map[string]any{"nested": map[string]any{"k": "v"}, "scalar": "x"}

	if got, err := Map(parent, "p", "nested"); err != nil {
		t.Fatalf("Map nested: %v", err)
	} else if got["k"] != "v" {
		t.Fatalf("Map nested: got %v", got)
	}

	if _, err := Map(parent, "p", "missing"); err == nil || !strings.Contains(err.Error(), "p.missing: missing") {
		t.Fatalf("Map missing: expected missing error, got %v", err)
	}

	if _, err := Map(parent, "p", "scalar"); err == nil || !strings.Contains(err.Error(), "expected map") {
		t.Fatalf("Map scalar: expected type error, got %v", err)
	}
}

func TestSlice(t *testing.T) {
	parent := map[string]any{"list": []any{1, 2, 3}, "scalar": "x"}

	if got, err := Slice(parent, "p", "list"); err != nil {
		t.Fatalf("Slice list: %v", err)
	} else if !reflect.DeepEqual(got, []any{1, 2, 3}) {
		t.Fatalf("Slice list: got %v", got)
	}

	if _, err := Slice(parent, "p", "missing"); err == nil {
		t.Fatal("Slice missing: expected error")
	}

	if _, err := Slice(parent, "p", "scalar"); err == nil || !strings.Contains(err.Error(), "expected slice") {
		t.Fatalf("Slice scalar: expected type error, got %v", err)
	}
}

func TestString(t *testing.T) {
	parent := map[string]any{"s": "hello", "n": 5}

	if got, err := String(parent, "p", "s"); err != nil || got != "hello" {
		t.Fatalf("String hit: %q err=%v", got, err)
	}
	if _, err := String(parent, "p", "missing"); err == nil {
		t.Fatal("String missing: expected error")
	}
	if _, err := String(parent, "p", "n"); err == nil || !strings.Contains(err.Error(), "expected string") {
		t.Fatalf("String wrong type: %v", err)
	}
}

func TestOptionalString(t *testing.T) {
	parent := map[string]any{"set": "v", "nilv": nil, "n": 5}

	if got, err := OptionalString(parent, "p", "set"); err != nil || got != "v" {
		t.Fatalf("OptionalString set: %q err=%v", got, err)
	}
	if got, err := OptionalString(parent, "p", "missing"); err != nil || got != "" {
		t.Fatalf("OptionalString missing: want empty, got %q err=%v", got, err)
	}
	if got, err := OptionalString(parent, "p", "nilv"); err != nil || got != "" {
		t.Fatalf("OptionalString nil: want empty, got %q err=%v", got, err)
	}
	if _, err := OptionalString(parent, "p", "n"); err == nil {
		t.Fatal("OptionalString wrong type: expected error")
	}
}

func TestInt(t *testing.T) {
	parent := map[string]any{"i": 1, "i64": int64(2), "f": 3.0, "frac": 3.5, "s": "x"}

	for _, key := range []string{"i", "i64", "f"} {
		got, err := Int(parent, "p", key)
		if err != nil {
			t.Fatalf("Int %s: %v", key, err)
		}
		want := map[string]int64{"i": 1, "i64": 2, "f": 3}[key]
		if got != want {
			t.Fatalf("Int %s: want %d got %d", key, want, got)
		}
	}
	if _, err := Int(parent, "p", "frac"); err == nil {
		t.Fatal("Int fractional float: expected error")
	}
	if _, err := Int(parent, "p", "s"); err == nil {
		t.Fatal("Int string: expected error")
	}
	if _, err := Int(parent, "p", "missing"); err == nil {
		t.Fatal("Int missing: expected error")
	}
}

func TestSortedKeys(t *testing.T) {
	got := SortedKeys(map[string]any{"b": 1, "a": 2, "c": 3})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SortedKeys: got %v want %v", got, want)
	}
}

func TestCloneMap(t *testing.T) {
	src := map[string]any{"a": 1, "b": []any{1, 2}}
	dst := CloneMap(src)
	dst["c"] = 3
	if _, ok := src["c"]; ok {
		t.Fatal("CloneMap: source mutated by clone write")
	}
	if dst["a"] != src["a"] || !reflect.DeepEqual(dst["b"], src["b"]) {
		t.Fatalf("CloneMap: contents diverged: %v vs %v", dst, src)
	}
}

func TestEndpointWithAddresses(t *testing.T) {
	endpoint := map[string]any{
		"protocol": "http",
		"host":     "127.0.0.1",
		"port":     int64(4242),
		"exposure": "loopback",
	}
	got, err := EndpointWithAddresses(endpoint)
	if err != nil {
		t.Fatalf("EndpointWithAddresses: %v", err)
	}
	if got["address"] != "127.0.0.1:4242" {
		t.Fatalf("address: got %v", got["address"])
	}
	if got["bind_address"] != "127.0.0.1:4242" {
		t.Fatalf("bind_address (default to host): got %v", got["bind_address"])
	}
	// Source endpoint must not be mutated.
	if _, ok := endpoint["address"]; ok {
		t.Fatal("EndpointWithAddresses mutated input")
	}
}

func TestEndpointWithAddresses_listenHostOverride(t *testing.T) {
	endpoint := map[string]any{
		"protocol":    "http",
		"host":        "10.0.0.1",
		"listen_host": "0.0.0.0",
		"port":        int64(443),
		"exposure":    "public",
	}
	got, err := EndpointWithAddresses(endpoint)
	if err != nil {
		t.Fatalf("EndpointWithAddresses: %v", err)
	}
	if got["address"] != "10.0.0.1:443" {
		t.Fatalf("address: got %v", got["address"])
	}
	if got["bind_address"] != "0.0.0.0:443" {
		t.Fatalf("bind_address: got %v", got["bind_address"])
	}
}

func TestYAMLDocument_headerAndDeterminism(t *testing.T) {
	payload := map[string]any{"b": 1, "a": map[string]any{"y": 2, "x": 1}}

	first, err := YAMLDocument(payload)
	if err != nil {
		t.Fatalf("YAMLDocument first: %v", err)
	}
	second, err := YAMLDocument(payload)
	if err != nil {
		t.Fatalf("YAMLDocument second: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("YAMLDocument is non-deterministic:\nfirst=%q\nsecond=%q", first, second)
	}
	if !strings.HasPrefix(string(first), Header) {
		t.Fatalf("YAMLDocument is missing the canonical header:\n%s", first)
	}
}
