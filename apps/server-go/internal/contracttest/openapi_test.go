package contracttest

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	OpenAPI string                 `yaml:"openapi"`
	Info    openAPIInfo            `yaml:"info"`
	Paths   map[string]openAPIPath `yaml:"paths"`
	Parts   openAPIComponents      `yaml:"components"`
}

type openAPIInfo struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

type openAPIComponents struct {
	Parameters map[string]openAPIParameter `yaml:"parameters"`
}

type openAPIPath struct {
	Parameters []openAPIParameter `yaml:"parameters"`
	Get        *openAPIOperation  `yaml:"get"`
	Post       *openAPIOperation  `yaml:"post"`
	Put        *openAPIOperation  `yaml:"put"`
	Delete     *openAPIOperation  `yaml:"delete"`
	Patch      *openAPIOperation  `yaml:"patch"`
	Options    *openAPIOperation  `yaml:"options"`
	Head       *openAPIOperation  `yaml:"head"`
	Trace      *openAPIOperation  `yaml:"trace"`
}

type openAPIOperation struct {
	OperationID string                     `yaml:"operationId"`
	Parameters  []openAPIParameter         `yaml:"parameters"`
	Security    []map[string][]string      `yaml:"security"`
	RequestBody *openAPIRequestBody        `yaml:"requestBody"`
	Responses   map[string]openAPIResponse `yaml:"responses"`
}

type openAPIRequestBody struct {
	Required bool           `yaml:"required"`
	Content  map[string]any `yaml:"content"`
}

type openAPIParameter struct {
	Ref      string `yaml:"$ref"`
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
}

type openAPIResponse struct {
	Content map[string]any `yaml:"content"`
}

type manifestOperation struct {
	ID          string
	Method      string
	Path        string
	Parameters  []openAPIParameter
	Security    []map[string][]string
	RequestBody *openAPIRequestBody
	Responses   map[string][]string
}

func TestOpenAPIExactlyDescribesFixtureOperations(t *testing.T) {
	fixture, err := Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPI(data, fixture); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAPIRejectsMalformedAndSemanticDrift(t *testing.T) {
	fixture, err := Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"malformed YAML", []byte("openapi: ["), "decode OpenAPI"},
		{"trailing YAML document", append(append([]byte{}, valid...), []byte("\n---\nopenapi: 3.1.0\n")...), "multiple YAML documents"},
		{"wrong OpenAPI version", []byte(strings.Replace(string(valid), "openapi: 3.1.0", "openapi: 3.0.3", 1)), "openapi ="},
		{"operation identity drift", []byte(strings.Replace(string(valid), "operationId: health.get", "operationId: health.changed", 1)), "operation set differs"},
		{"missing observed JSON response", []byte(strings.Replace(string(valid), "            application/json:\n", "            text/plain:\n", 1)), "application/json"},
		{"extra response status", []byte(strings.Replace(string(valid), "              schema: {}\n  /api/articles:", "              schema: {}\n        '418':\n          description: Extra\n          content:\n            application/json:\n              schema: {}\n  /api/articles:", 1)), "response statuses"},
		{"extra response media type", []byte(strings.Replace(string(valid), "            application/json:\n              schema: {}", "            application/json:\n              schema: {}\n            text/plain:\n              schema: {}", 1)), "response content types"},
		{"missing required path parameter", []byte(strings.Replace(string(valid), "      parameters:\n        - $ref: '#/components/parameters/Slug'\n", "", 1)), "required path parameter"},
		{"extra path parameter", []byte(strings.Replace(string(valid), "        - $ref: '#/components/parameters/Slug'\n", "        - $ref: '#/components/parameters/Slug'\n        - $ref: '#/components/parameters/ID'\n", 1)), "unexpected path parameter"},
		{"path parameter not required", []byte(strings.Replace(string(valid), "    Slug:\n      name: slug\n      in: path\n      required: true", "    Slug:\n      name: slug\n      in: path\n      required: false", 1)), "must be required"},
		{"missing observed query", []byte(strings.Replace(string(valid), "        - $ref: '#/components/parameters/Page'\n        - $ref: '#/components/parameters/Limit'\n", "        - $ref: '#/components/parameters/Limit'\n", 1)), "query parameter page"},
		{"required token made optional", []byte(strings.Replace(string(valid), "    Token:\n      name: token\n      in: query\n      required: true", "    Token:\n      name: token\n      in: query", 1)), "query parameter token must be required"},
		{"missing user security", []byte(strings.Replace(string(valid), "      security: &userSecurity\n        - bearerAuth: []", "      security: &userSecurity []", 1)), "security"},
		{"wrong cron security", []byte(strings.Replace(string(valid), "        - cronBearer: []", "        - bearerAuth: []", 1)), "cronBearer"},
		{"security on public route", []byte(strings.Replace(string(valid), "      operationId: health.get\n", "      operationId: health.get\n      security:\n        - bearerAuth: []\n", 1)), "public fixture request must not declare security"},
		{"JSON body media type drift", []byte(strings.Replace(string(valid), "      operationId: newsletter.subscribe\n      requestBody:\n        required: true\n        content:\n          application/json:", "      operationId: newsletter.subscribe\n      requestBody:\n        required: true\n        content:\n          text/plain:", 1)), "request content types"},
		{"missing required body marker", []byte(strings.Replace(string(valid), "      operationId: newsletter.subscribe\n      requestBody:\n        required: true", "      operationId: newsletter.subscribe\n      requestBody:", 1)), "request body must be required"},
		{"optional body made required", []byte(strings.Replace(string(valid), "      operationId: newsletter.unsubscribePost\n      requestBody:\n        content:", "      operationId: newsletter.unsubscribePost\n      requestBody:\n        required: true\n        content:", 1)), "request body must remain optional"},
		{"body documented for bodyless fixture", []byte(strings.Replace(string(valid), "      operationId: health.get\n", "      operationId: health.get\n      requestBody:\n        content:\n          application/json: {}\n", 1)), "must not declare requestBody"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOpenAPI(test.data, fixture)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateOpenAPI() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func validateOpenAPI(data []byte, fixture *Contract) error {
	operations, err := parseOpenAPIManifest(data)
	if err != nil {
		return err
	}
	actual := make(map[string]manifestOperation, len(operations))
	for _, operation := range operations {
		key := operation.ID + "|" + operation.Method + "|" + operation.Path
		if _, duplicate := actual[key]; duplicate {
			return fmt.Errorf("duplicate OpenAPI operation %s", key)
		}
		actual[key] = operation
	}
	expected := make(map[string]Operation, len(fixture.Operations))
	for _, operation := range fixture.Operations {
		key := operation.OperationID + "|" + lowerMethod(operation.Request.Method) + "|" + CanonicalPath(operation.Request.Path)
		expected[key] = operation
	}
	if len(actual) != 32 || len(expected) != 32 {
		return fmt.Errorf("operation counts are OpenAPI=%d fixture=%d, want 32", len(actual), len(expected))
	}
	for key, operation := range expected {
		documented, ok := actual[key]
		if !ok {
			return fmt.Errorf("OpenAPI operation set differs: missing %s", key)
		}
		if err := validateDocumentedOperation(documented, operation); err != nil {
			return fmt.Errorf("%s: %w", operation.OperationID, err)
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("OpenAPI operation set differs: unexpected %s", key)
		}
	}
	return nil
}

func parseOpenAPIManifest(data []byte) ([]manifestOperation, error) {
	var document openAPIDocument
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode OpenAPI YAML: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("decode trailing OpenAPI YAML: %w", err)
		}
		return nil, fmt.Errorf("multiple YAML documents are not allowed")
	}
	if document.OpenAPI != "3.1.0" {
		return nil, fmt.Errorf("openapi = %q, want 3.1.0", document.OpenAPI)
	}
	if strings.TrimSpace(document.Info.Title) == "" || strings.TrimSpace(document.Info.Version) == "" {
		return nil, fmt.Errorf("info title and version are required")
	}
	if len(document.Paths) == 0 {
		return nil, fmt.Errorf("paths contains no operations")
	}

	paths := make([]string, 0, len(document.Paths))
	for path := range document.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var operations []manifestOperation
	for _, path := range paths {
		item := document.Paths[path]
		methods := []struct {
			name      string
			operation *openAPIOperation
		}{
			{"get", item.Get}, {"post", item.Post}, {"put", item.Put}, {"delete", item.Delete},
			{"patch", item.Patch}, {"options", item.Options}, {"head", item.Head}, {"trace", item.Trace},
		}
		for _, method := range methods {
			if method.operation == nil {
				continue
			}
			operation := method.operation
			if strings.TrimSpace(operation.OperationID) == "" {
				return nil, fmt.Errorf("%s %s is missing operationId", method.name, path)
			}
			parameters, err := resolveParameters(append(append([]openAPIParameter{}, item.Parameters...), operation.Parameters...), document.Parts.Parameters)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", method.name, path, err)
			}
			if err := validatePathParameters(path, parameters); err != nil {
				return nil, fmt.Errorf("%s %s: %w", method.name, path, err)
			}
			responses := make(map[string][]string, len(operation.Responses))
			for status, response := range operation.Responses {
				for mediaType := range response.Content {
					responses[status] = append(responses[status], mediaType)
				}
				sort.Strings(responses[status])
			}
			operations = append(operations, manifestOperation{
				ID: operation.OperationID, Method: method.name, Path: path,
				Parameters: parameters, Security: operation.Security, RequestBody: operation.RequestBody, Responses: responses,
			})
		}
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf("paths contains no operations")
	}
	return operations, nil
}

var openAPIPathParameterPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

func resolveParameters(parameters []openAPIParameter, components map[string]openAPIParameter) ([]openAPIParameter, error) {
	resolved := make([]openAPIParameter, 0, len(parameters))
	for _, parameter := range parameters {
		if parameter.Ref != "" {
			const prefix = "#/components/parameters/"
			if !strings.HasPrefix(parameter.Ref, prefix) {
				return nil, fmt.Errorf("unsupported parameter reference %q", parameter.Ref)
			}
			name := strings.TrimPrefix(parameter.Ref, prefix)
			var ok bool
			parameter, ok = components[name]
			if !ok {
				return nil, fmt.Errorf("parameter reference %q not found", parameter.Ref)
			}
		}
		resolved = append(resolved, parameter)
	}
	return resolved, nil
}

func validatePathParameters(path string, parameters []openAPIParameter) error {
	expected := make(map[string]bool)
	for _, match := range openAPIPathParameterPattern.FindAllStringSubmatch(path, -1) {
		expected[match[1]] = true
	}
	declared := make(map[string]bool)
	for _, parameter := range parameters {
		if parameter.In != "path" {
			continue
		}
		if !expected[parameter.Name] {
			return fmt.Errorf("unexpected path parameter %q", parameter.Name)
		}
		if !parameter.Required {
			return fmt.Errorf("path parameter %q must be required", parameter.Name)
		}
		if declared[parameter.Name] {
			return fmt.Errorf("duplicate path parameter %q", parameter.Name)
		}
		declared[parameter.Name] = true
	}
	for name := range expected {
		if !declared[name] {
			return fmt.Errorf("template {%s} has no declared required path parameter", name)
		}
	}
	return nil
}

var requiredRequestBodies = map[string]bool{
	"newsletter.subscribe":        true,
	"auth.login":                  true,
	"dashboard.articles.create":   true,
	"dashboard.articles.update":   true,
	"dashboard.categories.create": true,
	"dashboard.categories.update": true,
	"dashboard.media.upload":      true,
	"dashboard.settings.update":   true,
}

func validateDocumentedOperation(documented manifestOperation, fixture Operation) error {
	status := statusString(fixture.Response.Status)
	if len(documented.Responses) != 1 || documented.Responses[status] == nil {
		return fmt.Errorf("response statuses are %v, want exactly [%s]", sortedKeys(documented.Responses), status)
	}
	if got := documented.Responses[status]; len(got) != 1 || got[0] != "application/json" {
		return fmt.Errorf("response content types are %v, want exactly [application/json]", got)
	}

	expectedSecurity := ""
	switch headerValue(fixture.Request.Headers, "Authorization") {
	case "$AUTHORIZATION":
		expectedSecurity = "bearerAuth"
	case "$CRON_AUTHORIZATION":
		expectedSecurity = "cronBearer"
	}
	if expectedSecurity == "" {
		if len(documented.Security) != 0 {
			return fmt.Errorf("public fixture request must not declare security")
		}
	} else if len(documented.Security) != 1 || len(documented.Security[0]) != 1 || documented.Security[0][expectedSecurity] == nil {
		return fmt.Errorf("security must be exactly %s", expectedSecurity)
	}

	parameters := make(map[string]openAPIParameter)
	for _, parameter := range documented.Parameters {
		parameters[parameter.In+":"+parameter.Name] = parameter
	}
	for name := range fixture.Request.Query {
		parameter, ok := parameters["query:"+name]
		if !ok {
			return fmt.Errorf("query parameter %s is not declared", name)
		}
		if (fixture.OperationID == "newsletter.confirm" || fixture.OperationID == "newsletter.unsubscribeGet") && name == "token" && !parameter.Required {
			return fmt.Errorf("query parameter token must be required")
		}
	}

	expectedMediaType := ""
	if fixture.Request.Multipart != nil {
		expectedMediaType = "multipart/form-data"
	} else if len(fixture.Request.Body) > 0 {
		expectedMediaType = "application/json"
	}
	if expectedMediaType == "" {
		if documented.RequestBody != nil {
			return fmt.Errorf("bodyless fixture request must not declare requestBody")
		}
		return nil
	}
	if documented.RequestBody == nil {
		return fmt.Errorf("requestBody is missing")
	}
	mediaTypes := sortedKeys(documented.RequestBody.Content)
	if len(mediaTypes) != 1 || mediaTypes[0] != expectedMediaType {
		return fmt.Errorf("request content types are %v, want exactly [%s]", mediaTypes, expectedMediaType)
	}
	mustBeRequired := requiredRequestBodies[fixture.OperationID]
	if mustBeRequired && !documented.RequestBody.Required {
		return fmt.Errorf("request body must be required")
	}
	if !mustBeRequired && documented.RequestBody.Required {
		return fmt.Errorf("request body must remain optional")
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
