package contracttest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mutateFixture(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(fixtureBytes(t), &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadApprovedFixture(t *testing.T) {
	contract, err := Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if contract.SchemaVersion != 2 || contract.Source != "node-contract-server" || len(contract.Operations) != 32 {
		t.Fatalf("unexpected fixture metadata: %#v (%d operations)", contract, len(contract.Operations))
	}
	if contract.FixedClock.IsZero() {
		t.Fatal("fixedClock was not parsed")
	}
	health, ok := contract.Operation("health.get")
	var compact bytes.Buffer
	if err := json.Compact(&compact, health.Response.Body); err != nil {
		t.Fatal(err)
	}
	if !ok || compact.String() != `{"status":"ok"}` {
		t.Fatalf("health operation/body = %#v %s", health, health.Response.Body)
	}
}

func TestParseRejectsMalformedAndWrongSchema(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{"malformed", []byte(`{"schemaVersion":`), "decode contract"},
		{"trailing JSON value", append(fixtureBytes(t), []byte(` {}`)...), "trailing JSON value"},
		{"trailing garbage", append(fixtureBytes(t), []byte(` x`)...), "trailing data"},
		{"version", mutateFixture(t, func(v map[string]any) { v["schemaVersion"] = 1 }), "schemaVersion"},
		{"source", mutateFixture(t, func(v map[string]any) { v["source"] = "other" }), "node-contract-server"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.data)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidationRejectsOperationAndPathDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"duplicate operation", func(v map[string]any) {
			ops := v["operations"].([]any)
			ops[1].(map[string]any)["operationId"] = ops[0].(map[string]any)["operationId"]
		}, "duplicate operationId"},
		{"invalid method", func(v map[string]any) {
			v["operations"].([]any)[0].(map[string]any)["request"].(map[string]any)["method"] = "TRACE"
		}, "method"},
		{"invalid status", func(v map[string]any) {
			v["operations"].([]any)[0].(map[string]any)["response"].(map[string]any)["status"] = 99
		}, "status"},
		{"missing path parameter", func(v map[string]any) {
			delete(v["operations"].([]any)[3].(map[string]any)["request"].(map[string]any), "pathParameters")
		}, "path parameters"},
		{"extra path parameter", func(v map[string]any) {
			v["operations"].([]any)[0].(map[string]any)["request"].(map[string]any)["pathParameters"] = map[string]any{"id": 1}
		}, "path parameters"},
		{"bad actual path", func(v map[string]any) {
			v["operations"].([]any)[3].(map[string]any)["request"].(map[string]any)["actualPath"] = "/wrong"
		}, "actualPath"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(mutateFixture(t, test.edit))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidationRejectsDependenciesPlaceholdersAndMultipartDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"dangling dependency operation", func(v map[string]any) {
			v["operations"].([]any)[21].(map[string]any)["dependencies"].([]any)[0].(map[string]any)["operationId"] = "missing"
		}, "dependency operation"},
		{"dangling response pointer", func(v map[string]any) {
			v["operations"].([]any)[21].(map[string]any)["dependencies"].([]any)[0].(map[string]any)["responsePointer"] = "/missing"
		}, "responsePointer"},
		{"dangling request pointer", func(v map[string]any) {
			v["operations"].([]any)[21].(map[string]any)["dependencies"].([]any)[0].(map[string]any)["requestTarget"] = "/missing"
		}, "requestTarget"},
		{"unbound placeholder", func(v map[string]any) {
			v["operations"].([]any)[0].(map[string]any)["request"].(map[string]any)["headers"].(map[string]any)["x-test"] = "$UNKNOWN"
		}, "unbound placeholder"},
		{"invalid base64", func(v map[string]any) {
			v["operations"].([]any)[28].(map[string]any)["request"].(map[string]any)["multipart"].(map[string]any)["contentBase64"] = "!"
		}, "base64"},
		{"invalid multipart size", func(v map[string]any) {
			v["operations"].([]any)[28].(map[string]any)["request"].(map[string]any)["multipart"].(map[string]any)["size"] = 999
		}, "size"},
		{"invalid token hash", func(v map[string]any) {
			v["replay"].(map[string]any)["bindings"].([]any)[1].(map[string]any)["vector"].(map[string]any)["sha256"] = "abcd"
		}, "sha256"},
		{"unsupported resolver", func(v map[string]any) {
			v["replay"].(map[string]any)["bindings"].([]any)[0].(map[string]any)["resolver"].(map[string]any)["type"] = "magic"
		}, "unsupported resolver"},
		{"unsupported transform", func(v map[string]any) {
			v["replay"].(map[string]any)["bindings"].([]any)[7].(map[string]any)["resolver"].(map[string]any)["transform"] = "uppercase"
		}, "basename transform"},
		{"missing response content type", func(v map[string]any) {
			delete(v["operations"].([]any)[0].(map[string]any)["response"].(map[string]any)["headers"].(map[string]any), "content-type")
		}, "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(mutateFixture(t, test.edit))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
