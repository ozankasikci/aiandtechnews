package editorial

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const tokenLifetime = 7 * 24 * time.Hour

var ErrInvalidToken = errors.New("invalid or expired token")

type BcryptVerifier struct{}

func NewBcryptVerifier() BcryptVerifier { return BcryptVerifier{} }

func (BcryptVerifier) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type Identity struct {
	ID    int64
	Email string
	Role  string
}

type Claims struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type JWT struct {
	secret []byte
	now    func() time.Time
}

func NewJWT(secret string, now func() time.Time) (*JWT, error) {
	if secret == "" {
		return nil, errors.New("JWT secret is required")
	}
	if now == nil {
		return nil, errors.New("clock is required")
	}
	return &JWT{secret: []byte(secret), now: now}, nil
}

func (j *JWT) Sign(identity Identity) (string, error) {
	iat := j.now().Unix()
	claims := Claims{
		ID: identity.ID, Email: identity.Email, Role: identity.Role,
		IssuedAt: iat, ExpiresAt: iat + int64(tokenLifetime/time.Second),
	}
	header, _ := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}{"HS256", "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", errors.New("encode token claims")
	}
	encode := base64.RawURLEncoding.EncodeToString
	input := encode(header) + "." + encode(payload)
	return input + "." + encode(j.signature(input)), nil
}

func (j *JWT) Verify(token string) (Claims, error) {
	parts := bytes.Split([]byte(token), []byte("."))
	if len(parts) != 3 || len(parts[0]) == 0 || len(parts[1]) == 0 || len(parts[2]) == 0 {
		return Claims{}, ErrInvalidToken
	}
	input := string(parts[0]) + "." + string(parts[1])
	signature, err := base64.RawURLEncoding.DecodeString(string(parts[2]))
	if err != nil || !hmac.Equal(signature, j.signature(input)) {
		return Claims{}, ErrInvalidToken
	}
	headerData, err := base64.RawURLEncoding.DecodeString(string(parts[0]))
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := decodeTokenJSON(headerData, &header); err != nil || header.Algorithm != "HS256" {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(string(parts[1]))
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := decodeTokenJSON(payload, &claims); err != nil || claims.ID <= 0 || claims.Email == "" || claims.Role == "" || claims.IssuedAt <= 0 || claims.ExpiresAt <= 0 || claims.ExpiresAt <= j.now().Unix() {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (j *JWT) signature(input string) []byte {
	mac := hmac.New(sha256.New, j.secret)
	_, _ = mac.Write([]byte(input))
	return mac.Sum(nil)
}

func decodeTokenJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalidToken
	}
	return nil
}
