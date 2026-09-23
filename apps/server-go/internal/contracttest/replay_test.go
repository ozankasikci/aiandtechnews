package contracttest

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func TestReplayComparesSemanticJSONAndSelectedHeaders(t *testing.T) {
	op := Operation{
		OperationID: "example",
		Request:     Request{Method: http.MethodPost, Path: "/items", Query: map[string]string{"n": "1"}, Headers: map[string]string{"x-literal": "yes"}, Body: []byte(`{"n":1}`)},
		Response:    Response{Status: 201, Headers: map[string]string{"content-type": "application/json"}, Body: []byte(`{"n":1,"nullable":null}`)},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/items?n=1" || r.Header.Get("X-Literal") != "yes" {
			t.Fatalf("request = %s %q", r.URL.String(), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"nullable":null,"n":1}`))
	})
	if err := Replay(handler, op); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayRejectsUnresolvedPlaceholder(t *testing.T) {
	op := Operation{OperationID: "placeholder", Request: Request{Method: "GET", Path: "/x", Headers: map[string]string{"authorization": "$TOKEN"}}, Response: Response{Status: 200, Body: []byte(`{}`)}}
	err := Replay(http.NotFoundHandler(), op)
	if err == nil || !strings.Contains(err.Error(), "unresolved placeholder") {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayReportsJSONTypeDifference(t *testing.T) {
	op := Operation{OperationID: "types", Request: Request{Method: "GET", Path: "/x"}, Response: Response{Status: 200, Body: []byte(`{"value":1}`)}}
	err := Replay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"value":"1"}`)) }), op)
	if err == nil || !strings.Contains(err.Error(), "JSON body") {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayComparesJSONNumbersByExactValue(t *testing.T) {
	op := Operation{OperationID: "numbers", Request: Request{Method: "GET", Path: "/x"}, Response: Response{Status: 200, Body: []byte(`{"integer":1,"decimal":0.1,"large":9007199254740993}`)}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"large":9007199254740993.0,"decimal":1e-1,"integer":1.0}`))
	})
	if err := Replay(handler, op); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayKeepsJSONScalarTypesDistinct(t *testing.T) {
	for _, actual := range []string{`{"value":"1"}`, `{"value":true}`, `{"value":null}`} {
		op := Operation{OperationID: "types", Request: Request{Method: "GET", Path: "/x"}, Response: Response{Status: 200, Body: []byte(`{"value":1}`)}}
		err := Replay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(actual)) }), op)
		if err == nil || !strings.Contains(err.Error(), "JSON body") {
			t.Fatalf("Replay() actual %s error = %v", actual, err)
		}
	}
}

func TestReplayRejectsTrailingJSON(t *testing.T) {
	op := Operation{OperationID: "trailing", Request: Request{Method: "GET", Path: "/x"}, Response: Response{Status: 200, Body: []byte(`{}`)}}
	err := Replay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{} {}`)) }), op)
	if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestReplayMultipartHeaderBoundaryMatchesEncodedBody(t *testing.T) {
	op := Operation{
		OperationID: "multipart",
		Request: Request{
			Method:  "POST",
			Path:    "/upload",
			Headers: map[string]string{"content-type": "multipart/form-data; boundary=reviewed-boundary"},
			Multipart: &Multipart{Field: "file", Filename: "safe.txt", MIMEType: "text/plain", Size: 4,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("safe"))},
		},
		Response: Response{Status: 200, Body: []byte(`{}`)},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Content-Type"), "boundary=reviewed-boundary") {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if header.Filename != "safe.txt" {
			t.Fatalf("filename = %q", header.Filename)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := Replay(handler, op); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
}

func TestExecuteThenVerifyAllowsResponseDerivedBindings(t *testing.T) {
	op := Operation{
		OperationID: "derived",
		Request:     Request{Method: "POST", Path: "/upload"},
		Response:    Response{Status: 201, Body: []byte(`{"url":"/uploads/$UPLOAD_FILENAME"}`)},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"/uploads/random.png"}`))
	})
	if err := Replay(handler, op); err == nil {
		t.Fatal("Replay() compared an unresolved response placeholder as equal")
	}
	response, err := Execute(handler, op)
	if err != nil {
		t.Fatal(err)
	}
	op.Response.Body = []byte(`{"url":"/uploads/random.png"}`)
	if err := Verify(op, response); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	op.Response.Status = 200
	if err := Verify(op, response); err == nil || !strings.Contains(err.Error(), "status = 201, want 200") {
		t.Fatalf("Verify() status error = %v", err)
	}
}

func TestExecuteRejectsUnresolvedRequestPlaceholders(t *testing.T) {
	op := Operation{OperationID: "unresolved", Request: Request{Method: "GET", Path: "/x", Headers: map[string]string{"authorization": "$AUTHORIZATION"}}, Response: Response{Status: 200, Body: []byte(`{}`)}}
	if _, err := Execute(http.NotFoundHandler(), op); err == nil || !strings.Contains(err.Error(), "unresolved placeholder $AUTHORIZATION") {
		t.Fatalf("Execute() error = %v", err)
	}
}
