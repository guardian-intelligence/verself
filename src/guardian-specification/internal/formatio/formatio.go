package formatio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	toon "github.com/toon-format/toon-go"
	"gopkg.in/yaml.v3"
)

func DecodeFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if err := Decode(data, filepath.Ext(path), out); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Ext(path), err)
	}
	return nil
}

func Decode(data []byte, format string, out any) error {
	switch formatKey(format) {
	case ".json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(out); err != nil {
			return err
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				return fmt.Errorf("multiple JSON documents are not supported")
			}
			return err
		}
		return nil
	case ".toml":
		md, err := toml.Decode(string(data), out)
		if err != nil {
			return err
		}
		for _, undecoded := range md.Undecoded() {
			if isOpenResourceSpecTOMLField(undecoded) {
				continue
			}
			return fmt.Errorf("unknown TOML field %q", undecoded.String())
		}
		return nil
	case ".toon":
		return toon.Unmarshal(data, out)
	case ".yaml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(out); err != nil {
			return err
		}
		var extra yaml.Node
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				return fmt.Errorf("multiple YAML documents are not supported")
			}
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported encoding %q", format)
	}
}

func isOpenResourceSpecTOMLField(key toml.Key) bool {
	// Component-owned CRD specs are intentionally open to the base parser.
	parts := strings.Split(key.String(), ".")
	return len(parts) >= 3 && parts[0] == "resources" && parts[1] == "spec"
}

func Write(w io.Writer, format string, value any) error {
	switch formatKey(format) {
	case ".json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	case ".toml":
		return toml.NewEncoder(w).Encode(value)
	case ".toon":
		data, err := toon.Marshal(value, toon.WithIndent(2))
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		_, err = io.WriteString(w, "\n")
		return err
	case ".yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer func() { _ = enc.Close() }()
		return enc.Encode(value)
	default:
		return fmt.Errorf("unsupported encoding %q", format)
	}
}

func formatKey(format string) string {
	key := strings.ToLower(strings.TrimSpace(format))
	if key == "" {
		return ""
	}
	if !strings.HasPrefix(key, ".") {
		key = "." + key
	}
	if key == ".yml" {
		return ".yaml"
	}
	return key
}
