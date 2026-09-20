package contracttest

import (
	"fmt"
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
	Responses   map[string]openAPIResponse `yaml:"responses"`
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
	ID        string
	Method    string
	Path      string
	Responses map[string]bool
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
		{"wrong OpenAPI version", []byte(strings.Replace(string(valid), "openapi: 3.1.0", "openapi: 3.0.3", 1)), "openapi ="},
		{"operation identity drift", []byte(strings.Replace(string(valid), "operationId: health.get", "operationId: health.changed", 1)), "operation set differs"},
		{"missing observed JSON response", []byte(strings.Replace(string(valid), "            application/json:\n", "            text/plain:\n", 1)), "application/json"},
		{"missing required path parameter", []byte(strings.Replace(string(valid), "      parameters:\n        - $ref: '#/components/parameters/Slug'\n", "", 1)), "required path parameter"},
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
		status := statusString(operation.Response.Status)
		if !documented.Responses[status] {
			return fmt.Errorf("%s observed status %s is missing application/json", operation.OperationID, status)
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
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode OpenAPI YAML: %w", err)
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
			parameters := append(append([]openAPIParameter{}, item.Parameters...), operation.Parameters...)
			if err := validatePathParameters(path, parameters, document.Parts.Parameters); err != nil {
				return nil, fmt.Errorf("%s %s: %w", method.name, path, err)
			}
			responses := make(map[string]bool, len(operation.Responses))
			for status, response := range operation.Responses {
				_, responses[status] = response.Content["application/json"]
			}
			operations = append(operations, manifestOperation{ID: operation.OperationID, Method: method.name, Path: path, Responses: responses})
		}
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf("paths contains no operations")
	}
	return operations, nil
}

var openAPIPathParameterPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]*)\}`)

func validatePathParameters(path string, parameters []openAPIParameter, components map[string]openAPIParameter) error {
	declared := make(map[string]bool)
	for _, parameter := range parameters {
		if parameter.Ref != "" {
			const prefix = "#/components/parameters/"
			if !strings.HasPrefix(parameter.Ref, prefix) {
				return fmt.Errorf("unsupported parameter reference %q", parameter.Ref)
			}
			name := strings.TrimPrefix(parameter.Ref, prefix)
			var ok bool
			parameter, ok = components[name]
			if !ok {
				return fmt.Errorf("parameter reference %q not found", parameter.Ref)
			}
		}
		if parameter.In == "path" && parameter.Required {
			declared[parameter.Name] = true
		}
	}
	for _, match := range openAPIPathParameterPattern.FindAllStringSubmatch(path, -1) {
		if !declared[match[1]] {
			return fmt.Errorf("template {%s} has no declared required path parameter", match[1])
		}
	}
	return nil
}
