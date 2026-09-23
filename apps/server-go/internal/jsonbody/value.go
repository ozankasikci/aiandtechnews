// Package jsonbody reproduces the request-body semantics the Node dashboard
// relies on: express.json() parsing, JavaScript truthiness, nullish and
// String() coercion, and better-sqlite3 parameter binding. It lets ported
// handlers keep Node's exact validation order and error behavior without
// guessing at Go struct types for loosely typed JSON input.
package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// ErrUnbindable reports a value better-sqlite3 refuses to bind (booleans,
// objects, and arrays that do not spread into exactly one scalar). Node turns
// that exception into an HTTP 500.
var ErrUnbindable = errors.New("value cannot be bound as a SQLite parameter")

// Value is one property of a parsed JSON body. The zero Value is JavaScript
// undefined (the property was absent).
type Value struct{ raw json.RawMessage }

// FromRaw wraps one JSON value.
func FromRaw(raw json.RawMessage) Value {
	return Value{raw: bytes.TrimSpace(raw)}
}

// String returns the JSON string value s.
func String(s string) Value {
	raw, _ := json.Marshal(s)
	return Value{raw: raw}
}

// Null returns the JSON null value.
func Null() Value { return Value{raw: json.RawMessage("null")} }

// Defined reports whether the property was present (value !== undefined).
func (v Value) Defined() bool { return len(v.raw) > 0 }

// IsNull reports value === null.
func (v Value) IsNull() bool { return string(v.raw) == "null" }

// Nullish reports value === null || value === undefined.
func (v Value) Nullish() bool { return !v.Defined() || v.IsNull() }

// Or implements the JavaScript ?? operator.
func (v Value) Or(fallback Value) Value {
	if v.Nullish() {
		return fallback
	}
	return v
}

func (v Value) kind() byte {
	if !v.Defined() {
		return 0
	}
	return v.raw[0]
}

// Str returns the value when typeof value === "string".
func (v Value) Str() (string, bool) {
	if v.kind() != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(v.raw, &s) != nil {
		return "", false
	}
	return s, true
}

// StringOrEmpty implements typeof value === "string" ? value : "".
func (v Value) StringOrEmpty() string {
	s, _ := v.Str()
	return s
}

// Is reports value === s for a string s.
func (v Value) Is(s string) bool {
	got, ok := v.Str()
	return ok && got == s
}

// Bool returns the value when typeof value === "boolean".
func (v Value) Bool() (bool, bool) {
	switch v.kind() {
	case 't':
		return true, true
	case 'f':
		return false, true
	}
	return false, false
}

func (v Value) number() (float64, bool) {
	switch k := v.kind(); {
	case k == '-' || (k >= '0' && k <= '9'):
		f, err := strconv.ParseFloat(string(v.raw), 64)
		var numErr *strconv.NumError
		if err != nil && !(errors.As(err, &numErr) && numErr.Err == strconv.ErrRange) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Truthy implements JavaScript truthiness for JSON values.
func (v Value) Truthy() bool {
	switch v.kind() {
	case 0, 'n', 'f':
		return false
	case 't', '{', '[':
		return true
	case '"':
		s, _ := v.Str()
		return s != ""
	default:
		f, ok := v.number()
		return ok && f != 0
	}
}

// JSString implements JavaScript String(value).
func (v Value) JSString() string {
	switch v.kind() {
	case 0:
		return "undefined"
	case 'n':
		return "null"
	case 't':
		return "true"
	case 'f':
		return "false"
	case '"':
		s, _ := v.Str()
		return s
	case '{':
		return "[object Object]"
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(v.raw, &items) != nil {
			return ""
		}
		parts := make([]string, len(items))
		for i, item := range items {
			// Array.prototype.join renders null and undefined elements as "".
			if element := FromRaw(item); !element.Nullish() {
				parts[i] = element.JSString()
			}
		}
		return strings.Join(parts, ",")
	default:
		f, _ := v.number()
		return jsNumber(f)
	}
}

// Bind converts the value the way better-sqlite3 binds a JavaScript argument:
// strings bind as TEXT, every number binds as REAL (so SQLite stores 5 in a
// TEXT column as "5.0"), null binds NULL, and a one-element array is spread
// into its element. Booleans, objects, undefined and other arrays fail.
func (v Value) Bind() (any, error) {
	switch v.kind() {
	case 'n':
		return nil, nil
	case '"':
		s, _ := v.Str()
		return s, nil
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(v.raw, &items) != nil || len(items) != 1 {
			return nil, ErrUnbindable
		}
		element := FromRaw(items[0])
		if element.kind() == '[' {
			return nil, ErrUnbindable
		}
		return element.Bind()
	case 0, 't', 'f', '{':
		return nil, ErrUnbindable
	default:
		f, ok := v.number()
		if !ok {
			return nil, ErrUnbindable
		}
		return f, nil
	}
}

// jsNumber implements Number.prototype.toString() for finite and infinite values.
func jsNumber(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	return mantissa + "e" + exponent[:1] + strings.TrimLeft(exponent[1:], "0")
}
