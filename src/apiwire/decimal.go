package apiwire

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

const (
	decimalUint64Pattern = "^[0-9]+$"
	decimalInt64Pattern  = "^-?[0-9]+$"
)

type DecimalUint64 struct {
	value uint64
}

type DecimalInt64 struct {
	value int64
}

func Uint64(value uint64) DecimalUint64 {
	return DecimalUint64{value: value}
}

func Int64(value int64) DecimalInt64 {
	return DecimalInt64{value: value}
}

func ParseUint64(value string) (uint64, error) {
	if !isUnsignedDecimal(value) {
		return 0, fmt.Errorf("apiwire: invalid uint64 decimal %q", value)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("apiwire: invalid uint64 decimal %q: %w", value, err)
	}
	return parsed, nil
}

func ParseInt64(value string) (int64, error) {
	if !isSignedDecimal(value) {
		return 0, fmt.Errorf("apiwire: invalid int64 decimal %q", value)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("apiwire: invalid int64 decimal %q: %w", value, err)
	}
	return parsed, nil
}

func (d DecimalUint64) Uint64() uint64 {
	return d.value
}

func (d DecimalInt64) Int64() int64 {
	return d.value
}

func (d DecimalUint64) String() string {
	return strconv.FormatUint(d.value, 10)
}

func (d DecimalInt64) String() string {
	return strconv.FormatInt(d.value, 10)
}

func (d DecimalUint64) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *DecimalUint64) UnmarshalText(text []byte) error {
	parsed, err := ParseUint64(string(text))
	if err != nil {
		return err
	}
	*d = Uint64(parsed)
	return nil
}

func (d DecimalUint64) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *DecimalUint64) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("apiwire: decimal uint64 cannot be null")
	}
	var value string
	// Reject JSON numbers; JS clients cannot represent all uint64 values safely.
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("apiwire: decimal uint64 must be a quoted decimal string: %w", err)
	}
	parsed, err := ParseUint64(value)
	if err != nil {
		return err
	}
	*d = Uint64(parsed)
	return nil
}

func (d DecimalUint64) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type:    huma.TypeString,
		Pattern: decimalUint64Pattern,
	}
}

func (d DecimalInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d DecimalInt64) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *DecimalInt64) UnmarshalText(text []byte) error {
	parsed, err := ParseInt64(string(text))
	if err != nil {
		return err
	}
	*d = Int64(parsed)
	return nil
}

func (d *DecimalInt64) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("apiwire: decimal int64 cannot be null")
	}
	var value string
	// Reject JSON numbers; JS clients cannot represent all int64 values safely.
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("apiwire: decimal int64 must be a quoted decimal string: %w", err)
	}
	parsed, err := ParseInt64(value)
	if err != nil {
		return err
	}
	*d = Int64(parsed)
	return nil
}

func (d DecimalInt64) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{
		Type:    huma.TypeString,
		Pattern: decimalInt64Pattern,
	}
}

var (
	_ json.Marshaler           = DecimalUint64{}
	_ json.Unmarshaler         = (*DecimalUint64)(nil)
	_ encoding.TextMarshaler   = DecimalUint64{}
	_ encoding.TextUnmarshaler = (*DecimalUint64)(nil)
	_ huma.SchemaProvider      = DecimalUint64{}
	_ json.Marshaler           = DecimalInt64{}
	_ json.Unmarshaler         = (*DecimalInt64)(nil)
	_ encoding.TextMarshaler   = DecimalInt64{}
	_ encoding.TextUnmarshaler = (*DecimalInt64)(nil)
	_ huma.SchemaProvider      = DecimalInt64{}
)

func isUnsignedDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isSignedDecimal(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
		if value == "" {
			return false
		}
	}
	return isUnsignedDecimal(value)
}
