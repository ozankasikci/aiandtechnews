// Package contracttest provides typed loading, validation, and in-process replay
// of the reviewed Node compatibility fixture. Production packages must not import it.
package contracttest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = 2

type Contract struct {
	SchemaVersion int             `json:"schemaVersion"`
	Source        string          `json:"source"`
	FixedClock    time.Time       `json:"fixedClock"`
	Replay        ReplayConfig    `json:"replay"`
	Normalization Normalization   `json:"normalization"`
	Operations    []Operation     `json:"operations"`
	Observations  json.RawMessage `json:"observations,omitempty"`
}

type ReplayConfig struct {
	Bindings []Binding `json:"bindings"`
}
type Normalization struct {
	Strategy                 string   `json:"strategy"`
	FixedValuesRemainLiteral []string `json:"fixedValuesRemainLiteral"`
}
type Binding struct {
	Placeholder string          `json:"placeholder"`
	Resolver    Resolver        `json:"resolver"`
	DependsOn   []string        `json:"dependsOn,omitempty"`
	Sensitive   bool            `json:"sensitive,omitempty"`
	Vector      json.RawMessage `json:"vector,omitempty"`
}
type Resolver struct {
	Type         string `json:"type"`
	Name         string `json:"name,omitempty"`
	OperationID  string `json:"operationId,omitempty"`
	Pointer      string `json:"pointer,omitempty"`
	Value        string `json:"value,omitempty"`
	SecretRef    string `json:"secretRef,omitempty"`
	SubscriberID int    `json:"subscriberId,omitempty"`
	Purpose      string `json:"purpose,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	Transform    string `json:"transform,omitempty"`
}
type Operation struct {
	OperationID  string       `json:"operationId"`
	Request      Request      `json:"request"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Response     Response     `json:"response"`
}
type Request struct {
	Method         string                     `json:"method"`
	Path           string                     `json:"path"`
	ActualPath     string                     `json:"actualPath,omitempty"`
	PathParameters map[string]json.RawMessage `json:"pathParameters,omitempty"`
	Query          map[string]string          `json:"query,omitempty"`
	Headers        map[string]string          `json:"headers,omitempty"`
	Body           json.RawMessage            `json:"body,omitempty"`
	Multipart      *Multipart                 `json:"multipart,omitempty"`
}
type Multipart struct {
	Field         string `json:"field"`
	Filename      string `json:"filename"`
	MIMEType      string `json:"mimeType"`
	Size          int    `json:"size"`
	ContentBase64 string `json:"contentBase64"`
}
type Dependency struct {
	OperationID     string `json:"operationId"`
	ResponsePointer string `json:"responsePointer"`
	RequestTarget   string `json:"requestTarget"`
}
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body"`
}

func Load(path string) (*Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read contract: %w", err)
	}
	return Parse(data)
}

func Parse(data []byte) (*Contract, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var contract Contract
	if err := decoder.Decode(&contract); err != nil {
		return nil, fmt.Errorf("decode contract: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode contract: trailing JSON value")
		}
		return nil, fmt.Errorf("decode contract trailing data: %w", err)
	}
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return &contract, nil
}

func (c *Contract) Operation(id string) (Operation, bool) {
	for _, operation := range c.Operations {
		if operation.OperationID == id {
			return operation, true
		}
	}
	return Operation{}, false
}

func (c *Contract) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", c.SchemaVersion, SchemaVersion)
	}
	if c.Source != "node-contract-server" {
		return fmt.Errorf("source = %q, want node-contract-server", c.Source)
	}
	if c.FixedClock.IsZero() {
		return errors.New("fixedClock is required")
	}
	if len(c.Operations) != 32 {
		return fmt.Errorf("operations = %d, want 32", len(c.Operations))
	}

	bindings := make(map[string]Binding, len(c.Replay.Bindings))
	for _, binding := range c.Replay.Bindings {
		if !placeholderPattern.MatchString(binding.Placeholder) || placeholderPattern.FindString(binding.Placeholder) != binding.Placeholder {
			return fmt.Errorf("invalid binding placeholder %q", binding.Placeholder)
		}
		if _, exists := bindings[binding.Placeholder]; exists {
			return fmt.Errorf("duplicate binding %s", binding.Placeholder)
		}
		bindings[binding.Placeholder] = binding
		if err := validateResolver(binding); err != nil {
			return err
		}
		if len(binding.Vector) > 0 {
			var vector struct {
				SHA256 string `json:"sha256"`
			}
			if err := json.Unmarshal(binding.Vector, &vector); err != nil {
				return fmt.Errorf("binding %s vector: %w", binding.Placeholder, err)
			}
			if vector.SHA256 != "" && !sha256Pattern.MatchString(vector.SHA256) {
				return fmt.Errorf("binding %s has invalid sha256", binding.Placeholder)
			}
		}
	}
	for _, binding := range c.Replay.Bindings {
		for _, dependency := range binding.DependsOn {
			if _, ok := bindings[dependency]; !ok {
				return fmt.Errorf("binding %s depends on missing binding %s", binding.Placeholder, dependency)
			}
		}
	}

	operations := make(map[string]Operation, len(c.Operations))
	for _, operation := range c.Operations {
		if operation.OperationID == "" {
			return errors.New("operationId is required")
		}
		if _, exists := operations[operation.OperationID]; exists {
			return fmt.Errorf("duplicate operationId %q", operation.OperationID)
		}
		operations[operation.OperationID] = operation
		if err := validateRequest(operation); err != nil {
			return err
		}
		if operation.Response.Status < 100 || operation.Response.Status > 599 {
			return fmt.Errorf("operation %s has invalid status %d", operation.OperationID, operation.Response.Status)
		}
		if len(operation.Response.Body) == 0 || !json.Valid(operation.Response.Body) {
			return fmt.Errorf("operation %s has invalid response body", operation.OperationID)
		}
		contentType := headerValue(operation.Response.Headers, "Content-Type")
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			return fmt.Errorf("operation %s response content-type = %q, want application/json", operation.OperationID, contentType)
		}
	}
	for _, operation := range c.Operations {
		requestValue, _ := rawValue(operation.Request)
		for _, dependency := range operation.Dependencies {
			source, ok := operations[dependency.OperationID]
			if !ok {
				return fmt.Errorf("operation %s dependency operation %q not found", operation.OperationID, dependency.OperationID)
			}
			responseValue, _ := rawValue(source.Response.Body)
			if _, ok := jsonPointer(responseValue, dependency.ResponsePointer); !ok {
				return fmt.Errorf("operation %s dependency responsePointer %q not found", operation.OperationID, dependency.ResponsePointer)
			}
			if _, ok := jsonPointer(requestValue, dependency.RequestTarget); !ok {
				return fmt.Errorf("operation %s dependency requestTarget %q not found", operation.OperationID, dependency.RequestTarget)
			}
		}
	}
	for _, binding := range c.Replay.Bindings {
		if binding.Resolver.OperationID != "" {
			source, ok := operations[binding.Resolver.OperationID]
			if !ok {
				return fmt.Errorf("binding %s resolver operation %q not found", binding.Placeholder, binding.Resolver.OperationID)
			}
			responseValue, _ := rawValue(source.Response.Body)
			if _, ok := jsonPointer(responseValue, binding.Resolver.Pointer); !ok {
				return fmt.Errorf("binding %s resolver pointer %q not found", binding.Placeholder, binding.Resolver.Pointer)
			}
		}
	}
	usedData, _ := json.Marshal(struct {
		Replay     ReplayConfig `json:"replay"`
		Operations []Operation  `json:"operations"`
	}{c.Replay, c.Operations})
	for _, placeholder := range placeholderPattern.FindAllString(string(usedData), -1) {
		if _, ok := bindings[placeholder]; !ok {
			return fmt.Errorf("unbound placeholder %s", placeholder)
		}
	}
	return nil
}

func validateResolver(binding Binding) error {
	r := binding.Resolver
	require := func(ok bool, field string) error {
		if !ok {
			return fmt.Errorf("binding %s resolver %s requires %s", binding.Placeholder, r.Type, field)
		}
		return nil
	}
	if r.Transform != "" && r.Type != "responseJsonPointerTransform" {
		return fmt.Errorf("binding %s resolver type %s does not support transform %q", binding.Placeholder, r.Type, r.Transform)
	}
	switch r.Type {
	case "secretRef":
		return require(r.Name != "", "name")
	case "responseJsonPointer":
		return require(r.OperationID != "" && strings.HasPrefix(r.Pointer, "/"), "operationId and JSON pointer")
	case "template":
		return require(r.Value != "", "value")
	case "newsletterToken":
		if err := require(r.SecretRef != "" && r.SubscriberID > 0 && (r.Purpose == "confirm" || r.Purpose == "unsubscribe"), "secretRef, subscriberId, and supported purpose"); err != nil {
			return err
		}
		if _, err := time.Parse(time.RFC3339Nano, r.ExpiresAt); err != nil {
			return fmt.Errorf("binding %s resolver newsletterToken has invalid expiresAt: %w", binding.Placeholder, err)
		}
		return nil
	case "secretRefTemplate":
		return require(r.SecretRef != "" && r.Value != "", "secretRef and value")
	case "responseJsonPointerTransform":
		return require(r.OperationID != "" && strings.HasPrefix(r.Pointer, "/") && r.Transform == "basename", "operationId, JSON pointer, and basename transform")
	case "literal":
		return require(r.Value != "", "value")
	default:
		return fmt.Errorf("binding %s has unsupported resolver type %q", binding.Placeholder, r.Type)
	}
}

var placeholderPattern = regexp.MustCompile(`\$[A-Z][A-Z0-9_]*`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var templateParamPattern = regexp.MustCompile(`:([A-Za-z][A-Za-z0-9_]*)`)

func validateRequest(operation Operation) error {
	r := operation.Request
	switch r.Method {
	case "GET", "POST", "PUT", "DELETE":
	default:
		return fmt.Errorf("operation %s has invalid method %q", operation.OperationID, r.Method)
	}
	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("operation %s path must be absolute", operation.OperationID)
	}
	names := templateParamPattern.FindAllStringSubmatch(r.Path, -1)
	expected := make([]string, 0, len(names))
	for _, match := range names {
		expected = append(expected, match[1])
	}
	sort.Strings(expected)
	actual := make([]string, 0, len(r.PathParameters))
	for name := range r.PathParameters {
		actual = append(actual, name)
	}
	sort.Strings(actual)
	if strings.Join(expected, ",") != strings.Join(actual, ",") {
		return fmt.Errorf("operation %s path parameters are %v, want %v", operation.OperationID, actual, expected)
	}
	if len(expected) == 0 {
		if r.ActualPath != "" {
			return fmt.Errorf("operation %s has actualPath without path parameters", operation.OperationID)
		}
	} else {
		if r.ActualPath == "" {
			return fmt.Errorf("operation %s actualPath is required", operation.OperationID)
		}
		reconstructed := r.Path
		for _, name := range expected {
			var scalar any
			if err := json.Unmarshal(r.PathParameters[name], &scalar); err != nil {
				return fmt.Errorf("operation %s path parameter %s: %w", operation.OperationID, name, err)
			}
			switch scalar.(type) {
			case string, float64, bool:
			default:
				return fmt.Errorf("operation %s path parameter %s must be scalar", operation.OperationID, name)
			}
			reconstructed = strings.ReplaceAll(reconstructed, ":"+name, url.PathEscape(fmt.Sprint(scalar)))
		}
		if reconstructed != r.ActualPath {
			return fmt.Errorf("operation %s actualPath %q does not reconstruct to %q", operation.OperationID, r.ActualPath, reconstructed)
		}
	}
	if len(r.Body) > 0 && !json.Valid(r.Body) {
		return fmt.Errorf("operation %s has invalid request body", operation.OperationID)
	}
	if r.Multipart != nil {
		decoded, err := base64.StdEncoding.DecodeString(r.Multipart.ContentBase64)
		if err != nil {
			return fmt.Errorf("operation %s multipart base64: %w", operation.OperationID, err)
		}
		if len(decoded) != r.Multipart.Size {
			return fmt.Errorf("operation %s multipart size = %d, decoded size = %d", operation.OperationID, r.Multipart.Size, len(decoded))
		}
		if r.Multipart.Field == "" || r.Multipart.Filename == "" || r.Multipart.MIMEType == "" {
			return fmt.Errorf("operation %s multipart metadata is incomplete", operation.OperationID)
		}
	}
	return nil
}

func CanonicalPath(path string) string { return templateParamPattern.ReplaceAllString(path, `{$1}`) }
func lowerMethod(method string) string { return strings.ToLower(method) }
func statusString(status int) string   { return strconv.Itoa(status) }

func rawValue(value any) (any, error) {
	var data []byte
	switch value := value.(type) {
	case json.RawMessage:
		data = value
	default:
		data, _ = json.Marshal(value)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func jsonPointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := value
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = node[token]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}
