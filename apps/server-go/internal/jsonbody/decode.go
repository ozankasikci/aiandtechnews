package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// MaxBytes is express.json()'s default "100kb" body limit.
const MaxBytes = 100 * 1024

// ErrInvalid reports a body express.json() would reject. Express answers with
// an HTML 400/413 page; the Go API answers {"error":"Invalid request body"}
// like the ported auth routes.
var ErrInvalid = errors.New("invalid request body")

// Object is a parsed JSON object body. Missing properties are undefined.
type Object map[string]Value

// Get returns the named property, or undefined.
func (o Object) Get(name string) Value { return o[name] }

// Decode reads r's body with express.json() default semantics: a request
// whose Content-Type is not application/json, or whose body is empty, parses
// as {}; strict mode rejects anything that does not start with { or [; a
// JSON array parses but has no named properties.
func Decode(w http.ResponseWriter, r *http.Request) (Object, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Body == nil {
		return Object{}, nil
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBytes))
	if err != nil {
		return nil, ErrInvalid
	}
	trimmed := bytes.TrimLeft(data, " \t\n\r")
	if len(trimmed) == 0 {
		return Object{}, nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, ErrInvalid
	}
	if !json.Valid(data) {
		return nil, ErrInvalid
	}
	if trimmed[0] == '[' {
		return Object{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, ErrInvalid
	}
	object := make(Object, len(fields))
	for name, raw := range fields {
		object[name] = FromRaw(raw)
	}
	return object, nil
}
