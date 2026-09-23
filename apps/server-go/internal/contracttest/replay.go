package contracttest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
)

// Replay executes one literal, fully resolved fixture operation against handler.
// It compares only fixture-selected headers, plus exact status and semantic JSON.
func Replay(handler http.Handler, operation Operation) error {
	response, err := Execute(handler, operation)
	if err != nil {
		return err
	}
	return Verify(operation, response)
}

// Execute sends one literal, fully resolved fixture request to handler and
// returns the recorded response without comparing it. Callers that must derive
// a binding from the response (such as $UPLOAD_FILENAME) resolve it into the
// expected response and then call Verify.
func Execute(handler http.Handler, operation Operation) (*httptest.ResponseRecorder, error) {
	if handler == nil {
		return nil, fmt.Errorf("replay %s: handler is nil", operation.OperationID)
	}
	requestData, _ := json.Marshal(operation.Request)
	if placeholder := placeholderPattern.Find(requestData); placeholder != nil {
		return nil, fmt.Errorf("replay %s: unresolved placeholder %s", operation.OperationID, placeholder)
	}

	target := operation.Request.Path
	if operation.Request.ActualPath != "" {
		target = operation.Request.ActualPath
	}
	// url.Values provides deterministic ordering and correct escaping.
	if len(operation.Request.Query) > 0 {
		values := make(mapValues, len(operation.Request.Query))
		for name, value := range operation.Request.Query {
			values[name] = value
		}
		target += "?" + values.Encode()
	}

	var body bytes.Buffer
	multipartContentType := ""
	if operation.Request.Multipart != nil {
		part := operation.Request.Multipart
		writer := multipart.NewWriter(&body)
		if supplied := headerValue(operation.Request.Headers, "Content-Type"); supplied != "" {
			mediaType, parameters, err := mime.ParseMediaType(supplied)
			if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") || parameters["boundary"] == "" {
				return nil, fmt.Errorf("replay %s: invalid multipart content-type %q", operation.OperationID, supplied)
			}
			if err := writer.SetBoundary(parameters["boundary"]); err != nil {
				return nil, fmt.Errorf("replay %s multipart boundary: %w", operation.OperationID, err)
			}
		}
		multipartContentType = writer.FormDataContentType()
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, part.Field, part.Filename))
		header.Set("Content-Type", part.MIMEType)
		file, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		content, err := base64.StdEncoding.DecodeString(part.ContentBase64)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(content); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
	} else if len(operation.Request.Body) > 0 {
		body.Write(operation.Request.Body)
	}

	request := httptest.NewRequest(operation.Request.Method, target, &body)
	for name, value := range operation.Request.Headers {
		request.Header.Set(name, value)
	}
	if multipartContentType != "" {
		request.Header.Set("Content-Type", multipartContentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, nil
}

// Verify compares a recorded response with the fixture's exact status,
// selected headers, and semantic JSON body.
func Verify(operation Operation, response *httptest.ResponseRecorder) error {
	if response == nil {
		return fmt.Errorf("replay %s: response is nil", operation.OperationID)
	}
	if response.Code != operation.Response.Status {
		return fmt.Errorf("replay %s: status = %d, want %d", operation.OperationID, response.Code, operation.Response.Status)
	}
	for name, expected := range operation.Response.Headers {
		if got := response.Header().Get(name); got != expected {
			return fmt.Errorf("replay %s: header %s = %q, want %q", operation.OperationID, name, got, expected)
		}
	}
	want, err := decodeJSON(operation.Response.Body)
	if err != nil {
		return fmt.Errorf("replay %s expected JSON body: %w", operation.OperationID, err)
	}
	got, err := decodeJSON(response.Body.Bytes())
	if err != nil {
		return fmt.Errorf("replay %s actual JSON body: %w", operation.OperationID, err)
	}
	if !semanticJSONEqual(got, want) {
		return fmt.Errorf("replay %s: JSON body = %s, want %s", operation.OperationID, response.Body.Bytes(), operation.Response.Body)
	}
	return nil
}

func semanticJSONEqual(left, right any) bool {
	switch left := left.(type) {
	case json.Number:
		right, ok := right.(json.Number)
		if !ok {
			return false
		}
		leftValue, leftOK := new(big.Rat).SetString(left.String())
		rightValue, rightOK := new(big.Rat).SetString(right.String())
		return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
	case map[string]any:
		right, ok := right.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, exists := right[key]
			if !exists || !semanticJSONEqual(value, other) {
				return false
			}
		}
		return true
	case []any:
		right, ok := right.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for index := range left {
			if !semanticJSONEqual(left[index], right[index]) {
				return false
			}
		}
		return true
	case string:
		right, ok := right.(string)
		return ok && left == right
	case bool:
		right, ok := right.(bool)
		return ok && left == right
	case nil:
		return right == nil
	default:
		return false
	}
}

type mapValues map[string]string

func (values mapValues) Encode() string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, queryEscape(key)+"="+queryEscape(value))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

func queryEscape(value string) string {
	// QueryEscape's plus-for-space behavior matches net/url request parsing.
	return strings.ReplaceAll(url.QueryEscape(value), "%20", "+")
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return value, nil
}

func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}
