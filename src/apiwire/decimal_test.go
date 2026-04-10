package apiwire

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestDecimalUint64JSON(t *testing.T) {
	encoded, err := json.Marshal(Uint64(math.MaxUint64))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `"18446744073709551615"` {
		t.Fatalf("expected quoted max uint64, got %s", encoded)
	}

	var decoded DecimalUint64
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal quoted decimal: %v", err)
	}
	if decoded.Uint64() != math.MaxUint64 {
		t.Fatalf("expected max uint64, got %d", decoded.Uint64())
	}

	if err := json.Unmarshal([]byte(`18446744073709551615`), &decoded); err == nil {
		t.Fatal("expected unquoted JSON number to fail")
	}

	if err := decoded.UnmarshalText([]byte("18446744073709551615")); err != nil {
		t.Fatalf("unmarshal text: %v", err)
	}
	if decoded.Uint64() != math.MaxUint64 {
		t.Fatalf("expected text max uint64, got %d", decoded.Uint64())
	}
}

func TestDecimalInt64JSON(t *testing.T) {
	encoded, err := json.Marshal(Int64(math.MinInt64))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `"-9223372036854775808"` {
		t.Fatalf("expected quoted min int64, got %s", encoded)
	}

	var decoded DecimalInt64
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal quoted decimal: %v", err)
	}
	if decoded.Int64() != math.MinInt64 {
		t.Fatalf("expected min int64, got %d", decoded.Int64())
	}

	if _, err := ParseInt64("+1"); err == nil {
		t.Fatal("expected leading plus sign to fail")
	}

	var textDecoded DecimalInt64
	if err := textDecoded.UnmarshalText([]byte("-42")); err != nil {
		t.Fatalf("unmarshal text: %v", err)
	}
	if textDecoded.Int64() != -42 {
		t.Fatalf("expected text -42, got %d", textDecoded.Int64())
	}
}

func TestDecimalUint64HumaSchema(t *testing.T) {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	schema := registry.Schema(reflect.TypeFor[DecimalUint64](), false, "")
	if schema.Type != huma.TypeString {
		t.Fatalf("expected string schema, got %q", schema.Type)
	}
	if schema.Pattern != decimalUint64Pattern {
		t.Fatalf("expected decimal pattern, got %q", schema.Pattern)
	}
	if schema.Format != "" {
		t.Fatalf("expected no integer format, got %q", schema.Format)
	}
}
