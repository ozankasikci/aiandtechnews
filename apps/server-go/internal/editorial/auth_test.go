package editorial

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

const (
	contractPassword = "contract-test-password"
	contractHash     = "$2a$04$abcdefghijklmnopqrstuu.GAp6e4m7hQ19qGCjMlGaXpbrUyvyq6"
	contractSecret   = "synthetic-contract-jwt-secret-never-production"
)

func TestBcryptVerifierMatchesNodeHash(t *testing.T) {
	verifier := NewBcryptVerifier()
	if !verifier.Verify(contractHash, contractPassword) {
		t.Fatal("valid password was rejected")
	}
	if verifier.Verify(contractHash, "wrong-password") {
		t.Fatal("wrong password was accepted")
	}
	if verifier.Verify("not-a-bcrypt-hash", contractPassword) {
		t.Fatal("malformed hash was accepted")
	}
}

func TestJWTMatchesApprovedNodeVector(t *testing.T) {
	clock := func() time.Time { return time.Unix(1789905600, 999999999) }
	tokens, err := NewJWT(contractSecret, clock)
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Sign(Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(token))
	if got := hex.EncodeToString(digest[:]); got != "f075310d5db836008907399e2d08f182b0d5734e870aa3db14b715c69be4ae56" {
		t.Fatalf("token SHA-256 = %s", got)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d", len(parts))
	}
	claims, err := tokens.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	want := Claims{ID: 201, Email: "editorial@example.invalid", Role: "admin", IssuedAt: 1789905600, ExpiresAt: 1790510400}
	if claims != want {
		t.Fatalf("claims = %#v, want %#v", claims, want)
	}
}

func TestJWTRejectsUnsafeAndInvalidTokens(t *testing.T) {
	now := time.Unix(1789905600, 0)
	tokens, err := NewJWT(contractSecret, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	valid, err := tokens.Sign(Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewJWT("another-synthetic-secret", func() time.Time { return now })
	wrongSecret, _ := other.Sign(Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})

	cases := map[string]string{
		"malformed":        "not-a-jwt",
		"tampered":         valid[:len(valid)-1] + "x",
		"wrong secret":     wrongSecret,
		"wrong alg":        signedTestTokenWithHeader(t, contractSecret, `{"alg":"HS512","typ":"JWT"}`, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}`),
		"trailing header":  signedTestTokenWithHeader(t, contractSecret, `{"alg":"HS256","typ":"JWT"}{}`, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}`),
		"trailing payload": signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}{}`),
		"missing id":       signedTestToken(t, contractSecret, `{"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}`),
		"missing email":    signedTestToken(t, contractSecret, `{"id":201,"role":"admin","iat":1789905600,"exp":1790510400}`),
		"missing role":     signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","iat":1789905600,"exp":1790510400}`),
		"missing iat":      signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","exp":1790510400}`),
		"missing exp":      signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600}`),
		"fractional id":    signedTestToken(t, contractSecret, `{"id":201.5,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400}`),
		"fractional iat":   signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600.5,"exp":1790510400}`),
		"fractional exp":   signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789905600,"exp":1790510400.5}`),
		"exp boundary":     signedTestToken(t, contractSecret, `{"id":201,"email":"editorial@example.invalid","role":"admin","iat":1789300800,"exp":1789905600}`),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tokens.Verify(token); err == nil {
				t.Fatal("Verify() error = nil")
			}
		})
	}

	expired, _ := NewJWT(contractSecret, func() time.Time { return now.Add(-8 * 24 * time.Hour) })
	expiredToken, _ := expired.Sign(Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if _, err := tokens.Verify(expiredToken); err == nil {
		t.Fatal("expired token accepted")
	}
}

func signedTestToken(t *testing.T, secret, payload string) string {
	t.Helper()
	return signedTestTokenWithHeader(t, secret, `{"alg":"HS256","typ":"JWT"}`, payload)
}

func signedTestTokenWithHeader(t *testing.T, secret, header, payload string) string {
	t.Helper()
	encode := base64.RawURLEncoding.EncodeToString
	signingInput := encode([]byte(header)) + "." + encode([]byte(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + encode(mac.Sum(nil))
}

func TestJWTRequiresSecretAndDoesNotExposeIt(t *testing.T) {
	if _, err := NewJWT("", time.Now); err == nil {
		t.Fatal("NewJWT() error = nil")
	}
	secret := "sensitive-value-that-must-not-appear"
	tokens, err := NewJWT(secret, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tokens.Verify("bad")
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "bad") {
		t.Fatalf("unsafe verification error: %v", err)
	}
}
