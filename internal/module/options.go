package module

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// OptionError is a structured option-validation error (CLI exit code 2).
type OptionError struct {
	Key string
	Msg string
}

func (e OptionError) Error() string { return fmt.Sprintf("option %q: %s", e.Key, e.Msg) }

// ParseValue converts a CLI string into the canonical typed value for the schema.
func (s OptionSchema) ParseValue(raw string) (any, error) {
	switch s.Type {
	case "bool":
		return strconv.ParseBool(strings.TrimSpace(raw))
	case "int":
		return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	case "string":
		return raw, nil
	case "enum":
		v := strings.TrimSpace(raw)
		if !toSet(s.Values)[v] {
			return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(s.Values, ", "))
		}
		return v, nil
	case "list<string>", "list<enum>":
		if strings.TrimSpace(raw) == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if s.Type == "list<enum>" && !toSet(s.Values)[v] {
				return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(s.Values, ", "))
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown option type %q", s.Type)
	}
}

// ValidateOptions returns a new fully-populated, canonically-typed option map.
func ValidateOptions(schema map[string]OptionSchema, in map[string]any) (map[string]any, error) {
	for k := range in {
		if _, ok := schema[k]; !ok {
			return nil, OptionError{Key: k, Msg: "not a known option for this module"}
		}
	}
	out := make(map[string]any, len(schema))
	for key, spec := range schema {
		raw, present := in[key]
		if !present {
			out[key] = defaultFor(spec)
			continue
		}
		v, err := coerce(spec, raw)
		if err != nil {
			return nil, OptionError{Key: key, Msg: err.Error()}
		}
		out[key] = v
	}
	return out, nil
}

func defaultFor(spec OptionSchema) any {
	if spec.Default != nil {
		v, err := coerce(spec, spec.Default)
		if err == nil {
			return v
		}
	}
	switch spec.Type {
	case "bool":
		return false
	case "int":
		return int64(0)
	case "string", "enum":
		return ""
	default:
		return []string{}
	}
}

func coerce(spec OptionSchema, raw any) (any, error) {
	switch spec.Type {
	case "bool":
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean, got %T", raw)
		}
		return b, nil
	case "int":
		switch n := raw.(type) {
		case int64:
			return n, nil
		case int:
			return int64(n), nil
		default:
			return nil, fmt.Errorf("expected an integer, got %T", raw)
		}
	case "string":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", raw)
		}
		return s, nil
	case "enum":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", raw)
		}
		if !toSet(spec.Values)[s] {
			return nil, fmt.Errorf("%q is not one of %s", s, strings.Join(spec.Values, ", "))
		}
		return s, nil
	case "list<string>", "list<enum>":
		list, ok := toStringSlice(raw)
		if !ok {
			return nil, fmt.Errorf("expected a list of strings, got %T", raw)
		}
		if spec.Type == "list<enum>" {
			for _, v := range list {
				if !toSet(spec.Values)[v] {
					return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(spec.Values, ", "))
				}
			}
		}
		return append([]string{}, list...), nil
	default:
		return nil, fmt.Errorf("unknown option type %q", spec.Type)
	}
}

// OptionsHash is a deterministic content hash of a normalized option map.
func OptionsHash(normalized map[string]any) string {
	keys := make([]string, 0, len(normalized))
	for k := range normalized {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(normalized[k])
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')

	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
