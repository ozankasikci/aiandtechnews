package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret}
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

	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tokens, err := editorial.NewJWT(authTestSecret, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Sign(editorial.Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(token))
	if got := hex.EncodeToString(digest[:]); got != "f075310d5db836008907399e2d08f182b0d5734e870aa3db14b715c69be4ae56" {
		t.Fatalf("token SHA-256 = %s", got)
	}

	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[string]string{
		"$PASSWORD":      authTestPassword,
		"$JWT":           token,
		"$AUTHORIZATION": "Bearer " + token,
	}
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
	assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/auth/me", "", "Bearer "+token), http.StatusOK, `{"user":{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}}`)
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
