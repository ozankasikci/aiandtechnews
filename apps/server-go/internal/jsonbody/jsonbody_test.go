package jsonbody_test

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

func raw(s string) jsonbody.Value { return jsonbody.FromRaw([]byte(s)) }

func TestValueJavaScriptOperators(t *testing.T) {
	undefined := jsonbody.Value{}
	tests := []struct {
		name          string
		value         jsonbody.Value
		defined, null bool
		truthy        bool
		jsString      string
		stringOrEmpty string
	}{
		{"undefined", undefined, false, false, false, "undefined", ""},
		{"null", raw("null"), true, true, false, "null", ""},
		{"empty string", raw(`""`), true, false, false, "", ""},
		{"string", raw(`"a b"`), true, false, true, "a b", "a b"},
		{"zero", raw("0"), true, false, false, "0", ""},
		{"negative zero", raw("-0"), true, false, false, "0", ""},
		{"integer", raw("101"), true, false, true, "101", ""},
		{"fraction", raw("1.5"), true, false, true, "1.5", ""},
		{"large", raw("1e21"), true, false, true, "1e+21", ""},
		{"small", raw("1.5e-7"), true, false, true, "1.5e-7", ""},
		{"true", raw("true"), true, false, true, "true", ""},
		{"false", raw("false"), true, false, false, "false", ""},
		{"object", raw(`{"a":1}`), true, false, true, "[object Object]", ""},
		{"empty array", raw(`[]`), true, false, true, "", ""},
		{"array", raw(`["https://a.example/x", null, 2]`), true, false, true, "https://a.example/x,,2", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Defined(); got != tt.defined {
				t.Errorf("Defined = %v", got)
			}
			if got := tt.value.IsNull(); got != tt.null {
				t.Errorf("IsNull = %v", got)
			}
			if got := tt.value.Nullish(); got != (!tt.defined || tt.null) {
				t.Errorf("Nullish = %v", got)
			}
			if got := tt.value.Truthy(); got != tt.truthy {
				t.Errorf("Truthy = %v", got)
			}
			if got := tt.value.JSString(); got != tt.jsString {
				t.Errorf("JSString = %q", got)
			}
			if got := tt.value.StringOrEmpty(); got != tt.stringOrEmpty {
				t.Errorf("StringOrEmpty = %q", got)
			}
		})
	}
	if got := undefined.Or(jsonbody.String("x")); !got.Is("x") {
		t.Error("undefined ?? x != x")
	}
	if got := raw("null").Or(jsonbody.String("x")); !got.Is("x") {
		t.Error("null ?? x != x")
	}
	if got := raw(`""`).Or(jsonbody.String("x")); !got.Is("") {
		t.Error(`"" ?? x != ""`)
	}
	if b, ok := raw("true").Bool(); !ok || !b {
		t.Error("Bool(true)")
	}
	if _, ok := raw(`"true"`).Bool(); ok {
		t.Error(`Bool("true") reported a boolean`)
	}
}

func TestValueBindMatchesBetterSQLite3(t *testing.T) {
	for name, tt := range map[string]struct {
		value jsonbody.Value
		want  any
	}{
		"null":             {raw("null"), nil},
		"string":           {raw(`"x"`), "x"},
		"integer is REAL":  {raw("5"), float64(5)},
		"fraction":         {raw("1.5"), 1.5},
		"one element":      {raw(`["x"]`), "x"},
		"one null element": {raw(`[null]`), nil},
	} {
		got, err := tt.value.Bind()
		if err != nil || got != tt.want {
			t.Errorf("%s: Bind = %#v, %v; want %#v", name, got, err, tt.want)
		}
	}
	for name, value := range map[string]jsonbody.Value{
		"undefined": {}, "true": raw("true"), "false": raw("false"), "object": raw(`{}`),
		"empty array": raw(`[]`), "two elements": raw(`[1,2]`), "nested array": raw(`[[1]]`), "boolean element": raw(`[true]`),
	} {
		if _, err := value.Bind(); !errors.Is(err, jsonbody.ErrUnbindable) {
			t.Errorf("%s: Bind error = %v, want ErrUnbindable", name, err)
		}
	}
	if got, err := raw("1e400").Bind(); err != nil || !math.IsInf(got.(float64), 1) {
		t.Errorf("overflow Bind = %v, %v", got, err)
	}
}

func decode(t *testing.T, contentType, body string) (jsonbody.Object, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return jsonbody.Decode(httptest.NewRecorder(), request)
}

func TestDecodeMatchesExpressJSON(t *testing.T) {
	object, err := decode(t, "application/json; charset=utf-8", ` {"title":"T","n":null,"k":5} `)
	if err != nil || !object.Get("title").Is("T") || !object.Get("n").IsNull() || object.Get("missing").Defined() {
		t.Fatalf("object = %#v, %v", object, err)
	}
	for name, tt := range map[string]struct{ contentType, body string }{
		"no content type":   {"", `{"title":"T"}`},
		"text content type": {"text/plain", `{"title":"T"}`},
		"vendor json":       {"application/vnd.api+json", `{"title":"T"}`},
		"empty body":        {"application/json", ""},
		"whitespace body":   {"application/json", " \n"},
		"array body":        {"application/json", `["title"]`},
	} {
		object, err := decode(t, tt.contentType, tt.body)
		if err != nil || len(object) != 0 {
			t.Errorf("%s: object = %#v, %v; want {}", name, object, err)
		}
	}
	for name, body := range map[string]string{
		"malformed": `{`, "scalar string": `"x"`, "number": `5`, "null": `null`, "trailing value": `{"a":1}{}`,
		"oversized": `{"a":"` + strings.Repeat("x", jsonbody.MaxBytes) + `"}`,
	} {
		if _, err := decode(t, "application/json", body); !errors.Is(err, jsonbody.ErrInvalid) {
			t.Errorf("%s: error = %v, want ErrInvalid", name, err)
		}
	}
}
