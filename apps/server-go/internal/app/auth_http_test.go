package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

const (
	authTestSecret   = "synthetic-contract-jwt-secret-never-production"
	authTestPassword = "contract-test-password"
)

func authApplication(t *testing.T) (http.Handler, interface{ Close() error }) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret, UploadsDir: t.TempDir()}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return application.Handler(), db
}

func request(t *testing.T, handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Origin", "https://client.example.invalid")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertAuthResponse(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), status, body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS = %q", got)
	}
	if strings.HasSuffix(response.Body.String(), "\n") {
		t.Error("body has trailing newline")
	}
}

func TestAuthMatchesApprovedContractSequence(t *testing.T) {
	handler, db := authApplication(t)
	defer db.Close()

	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	passwordBinding, err := authBinding(contract.Replay.Bindings, "$PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	password, err := resolveAuthSecret(passwordBinding, map[string]string{"CONTRACT_TEST_PASSWORD": authTestPassword})
	if err != nil {
		t.Fatal(err)
	}

	login, ok := contract.Operation("auth.login")
	if !ok {
		t.Fatal("operation auth.login missing")
	}
	resolvedLoginRequest, err := resolveAuthOperation(login, map[string]string{"$PASSWORD": password})
	if err != nil {
		t.Fatalf("resolve auth.login request: %v", err)
	}
	loginResponse := executeAuthOperation(t, handler, resolvedLoginRequest)

	jwtBinding, err := authBinding(contract.Replay.Bindings, "$JWT")
	if err != nil {
		t.Fatal(err)
	}
	token, err := resolveAuthResponse(jwtBinding, login.OperationID, loginResponse.Body.Bytes())
	if err != nil {
		t.Fatalf("resolve $JWT from login response: %v", err)
	}
	if err := verifyAuthBindingVector(jwtBinding, token); err != nil {
		t.Fatal(err)
	}

	bindings := map[string]string{"$PASSWORD": password, "$JWT": token}
	resolvedLogin, err := resolveAuthOperation(login, bindings)
	if err != nil {
		t.Fatalf("resolve auth.login fixture: %v", err)
	}
	assertRecordedContractResponse(t, loginResponse, resolvedLogin)

	authorizationBinding, err := authBinding(contract.Replay.Bindings, "$AUTHORIZATION")
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := resolveAuthTemplate(authorizationBinding, bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings["$AUTHORIZATION"] = authorization

	previous := -1
	for _, id := range []string{"auth.login", "auth.me", "auth.logout"} {
		op, ok := contract.Operation(id)
		if !ok {
			t.Fatalf("operation %s missing", id)
		}
		if index := operationIndex(contract.Operations, id); index <= previous {
			t.Fatalf("operation %s is not in canonical order", id)
		} else {
			previous = index
		}
		if id == "auth.login" {
			continue
		}
		resolved, err := resolveAuthOperation(op, bindings)
		if err != nil {
			t.Fatalf("resolve %s fixture: %v", id, err)
		}
		if err := contracttest.Replay(handler, resolved); err != nil {
			// Replay body mismatches include literal expected/actual JSON. Do not
			// attach err here because the login body contains a sensitive JWT.
			t.Fatalf("%s contract replay failed (details redacted)", id)
		}
	}

	// Logout is client-side only and must not revoke a stateless JWT.
	me, _ := contract.Operation("auth.me")
	resolvedMe, err := resolveAuthOperation(me, bindings)
	if err != nil {
		t.Fatalf("resolve post-logout auth.me fixture: %v", err)
	}
	if err := contracttest.Replay(handler, resolvedMe); err != nil {
		t.Fatal("logout revoked the response-derived JWT (details redacted)")
	}
}

func authBinding(bindings []contracttest.Binding, placeholder string) (contracttest.Binding, error) {
	for _, binding := range bindings {
		if binding.Placeholder == placeholder {
			return binding, nil
		}
	}
	return contracttest.Binding{}, fmt.Errorf("binding %s missing", placeholder)
}

func resolveAuthSecret(binding contracttest.Binding, secrets map[string]string) (string, error) {
	resolver := binding.Resolver
	if resolver.Type != "secretRef" || resolver.Name == "" || resolver != (contracttest.Resolver{Type: "secretRef", Name: resolver.Name}) {
		return "", fmt.Errorf("binding %s has unsupported secret resolver", binding.Placeholder)
	}
	value, ok := secrets[resolver.Name]
	if !ok {
		return "", fmt.Errorf("binding %s references an unavailable test secret", binding.Placeholder)
	}
	return value, nil
}

func resolveAuthResponse(binding contracttest.Binding, operationID string, body []byte) (string, error) {
	resolver := binding.Resolver
	want := contracttest.Resolver{Type: "responseJsonPointer", OperationID: operationID, Pointer: "/token"}
	if resolver != want {
		return "", fmt.Errorf("binding %s has unsupported response resolver", binding.Placeholder)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		return "", fmt.Errorf("binding %s response is not valid JSON", binding.Placeholder)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("binding %s response has trailing JSON data", binding.Placeholder)
	}
	value, ok := response["token"].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("binding %s response pointer did not resolve to a string", binding.Placeholder)
	}
	return value, nil
}

func verifyAuthBindingVector(binding contracttest.Binding, value string) error {
	var vector struct {
		SHA256 string `json:"sha256"`
	}
	if len(binding.Vector) == 0 || json.Unmarshal(binding.Vector, &vector) != nil || vector.SHA256 == "" {
		return fmt.Errorf("binding %s has no supported SHA-256 vector", binding.Placeholder)
	}
	digest := sha256.Sum256([]byte(value))
	if got := hex.EncodeToString(digest[:]); got != vector.SHA256 {
		return fmt.Errorf("binding %s SHA-256 does not match its approved vector", binding.Placeholder)
	}
	return nil
}

func resolveAuthTemplate(binding contracttest.Binding, values map[string]string) (string, error) {
	resolver := binding.Resolver
	if resolver.Type != "template" || resolver.Value == "" || resolver != (contracttest.Resolver{Type: "template", Value: resolver.Value}) {
		return "", fmt.Errorf("binding %s has unsupported template resolver", binding.Placeholder)
	}
	if len(binding.DependsOn) != 1 || binding.DependsOn[0] != "$JWT" {
		return "", fmt.Errorf("binding %s has unsupported template dependencies", binding.Placeholder)
	}
	value := resolver.Value
	for _, dependency := range binding.DependsOn {
		resolved, ok := values[dependency]
		if !ok {
			return "", fmt.Errorf("binding %s has unresolved dependency %s", binding.Placeholder, dependency)
		}
		value = strings.ReplaceAll(value, "${"+dependency+"}", resolved)
	}
	if strings.Contains(value, "${") {
		return "", fmt.Errorf("binding %s template remains unresolved", binding.Placeholder)
	}
	return value, nil
}

func executeAuthOperation(t *testing.T, handler http.Handler, operation contracttest.Operation) *httptest.ResponseRecorder {
	t.Helper()
	target := operation.Request.Path
	if operation.Request.ActualPath != "" {
		target = operation.Request.ActualPath
	}
	if len(operation.Request.Query) > 0 {
		query := make(url.Values, len(operation.Request.Query))
		for name, value := range operation.Request.Query {
			query.Set(name, value)
		}
		target += "?" + query.Encode()
	}
	request := httptest.NewRequest(operation.Request.Method, target, bytes.NewReader(operation.Request.Body))
	for name, value := range operation.Request.Headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertRecordedContractResponse(t *testing.T, response *httptest.ResponseRecorder, operation contracttest.Operation) {
	t.Helper()
	recorded := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		for name, values := range response.Header() {
			for _, value := range values {
				writer.Header().Add(name, value)
			}
		}
		writer.WriteHeader(response.Code)
		_, _ = writer.Write(response.Body.Bytes())
	})
	if err := contracttest.Replay(recorded, operation); err != nil {
		t.Fatalf("%s contract response mismatch (details redacted)", operation.OperationID)
	}
}

func resolveAuthOperation(operation contracttest.Operation, bindings map[string]string) (contracttest.Operation, error) {
	data, err := json.Marshal(operation)
	if err != nil {
		return contracttest.Operation{}, err
	}
	for placeholder, value := range bindings {
		encoded, err := json.Marshal(value)
		if err != nil {
			return contracttest.Operation{}, err
		}
		data = bytes.ReplaceAll(data, []byte(`"`+placeholder+`"`), encoded)
	}
	var resolved contracttest.Operation
	if err := json.Unmarshal(data, &resolved); err != nil {
		return contracttest.Operation{}, err
	}
	return resolved, nil
}

func operationIndex(operations []contracttest.Operation, id string) int {
	for i, operation := range operations {
		if operation.OperationID == id {
			return i
		}
	}
	return -1
}

func TestAuthHTTPNegativeContracts(t *testing.T) {
	handler, db := authApplication(t)
	defer db.Close()

	for name, body := range map[string]string{
		"missing email":    `{"password":"x"}`,
		"missing password": `{"email":"editorial@example.invalid"}`,
		"empty fields":     `{"email":"","password":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertAuthResponse(t, request(t, handler, http.MethodPost, "/api/auth/login", body, ""), http.StatusBadRequest, `{"error":"Email and password are required"}`)
		})
	}
	for name, body := range map[string]string{
		"unknown email":  `{"email":"unknown@example.invalid","password":"x"}`,
		"wrong password": `{"email":"editorial@example.invalid","password":"wrong"}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertAuthResponse(t, request(t, handler, http.MethodPost, "/api/auth/login", body, ""), http.StatusUnauthorized, `{"error":"Invalid email or password"}`)
		})
	}
	for name, body := range map[string]string{
		"malformed": `{`,
		"trailing":  `{"email":"a","password":"b"}{}`,
		"oversized": `{"email":"a","password":"` + strings.Repeat("x", 100*1024) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertAuthResponse(t, request(t, handler, http.MethodPost, "/api/auth/login", body, ""), http.StatusBadRequest, `{"error":"Invalid request body"}`)
		})
	}
	for name, authorization := range map[string]string{"missing": "", "basic": "Basic abc", "wrong case": "bearer abc"} {
		t.Run(name, func(t *testing.T) {
			assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/auth/me", "", authorization), http.StatusUnauthorized, `{"error":"Authentication required"}`)
		})
	}
	for name, authorization := range map[string]string{"empty bearer": "Bearer ", "malformed": "Bearer not-a-jwt"} {
		t.Run(name, func(t *testing.T) {
			assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/auth/me", "", authorization), http.StatusUnauthorized, `{"error":"Invalid or expired token"}`)
		})
	}
	expiredTokens, err := editorial.NewJWT(authTestSecret, func() time.Time {
		return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := expiredTokens.Sign(editorial.Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/auth/me", "", "Bearer "+expired), http.StatusUnauthorized, `{"error":"Invalid or expired token"}`)
}

func TestAuthDatabaseFailureIsNonLeaking(t *testing.T) {
	handler, db := authApplication(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodPost, "/api/auth/login", `{"email":"editorial@example.invalid","password":"private-password"}`, "")
	assertAuthResponse(t, response, http.StatusInternalServerError, `{"error":"Internal server error"}`)
	if strings.Contains(response.Body.String(), "private-password") || strings.Contains(response.Body.String(), authTestSecret) || strings.Contains(response.Body.String(), "database") {
		t.Fatalf("response leaked sensitive/internal data: %q", response.Body.String())
	}
}
