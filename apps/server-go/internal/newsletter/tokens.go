package newsletter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Purpose is the token purpose Node signs into every link.
type Purpose string

const (
	PurposeConfirm     Purpose = "confirm"
	PurposeUnsubscribe Purpose = "unsubscribe"
)

// CreateToken is Node's createNewsletterToken (apps/server/src/newsletter/tokens.ts):
// base64url(JSON.stringify({v:1,id,purpose[,exp]})) + "." +
// base64url(HMAC-SHA256(secret, payload)), with exp in whole seconds.
func CreateToken(subscriberID int64, purpose Purpose, secret string, expiresAt *time.Time) string {
	var payload strings.Builder
	payload.WriteString(`{"v":1,"id":`)
	payload.WriteString(strconv.FormatInt(subscriberID, 10))
	payload.WriteString(`,"purpose":`)
	writeJSONString(&payload, string(purpose))
	if expiresAt != nil {
		payload.WriteString(`,"exp":`)
		payload.WriteString(strconv.FormatFloat(math.Floor(float64(expiresAt.UnixMilli())/1000), 'f', -1, 64))
	}
	payload.WriteByte('}')
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload.String()))
	return encoded + "." + sign(encoded, secret)
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyToken is Node's verifyNewsletterToken: it returns the subscriber id
// of a token signed with secret for purpose that has not expired at now.
// Like Node it accepts a token followed by "." or "..anything" (the third
// dot-separated segment must be empty), decodes the payload leniently like
// Buffer.from(value, "base64url"), and compares signatures in constant time.
func VerifyToken(token string, purpose Purpose, secret string, now time.Time) (int64, bool) {
	parts := strings.Split(token, ".")
	payload := parts[0]
	var signature, extra string
	if len(parts) > 1 {
		signature = parts[1]
	}
	if len(parts) > 2 {
		extra = parts[2]
	}
	if payload == "" || signature == "" || extra != "" {
		return 0, false
	}
	if !hmac.Equal([]byte(signature), []byte(sign(payload, secret))) {
		return 0, false
	}

	// JSON.parse: one JSON value (surrounding whitespace allowed) that must
	// be an object for the property reads below to succeed.
	var value map[string]json.RawMessage
	if err := json.Unmarshal(nodeBase64Decode(payload), &value); err != nil || value == nil {
		return 0, false
	}
	if version, ok := jsNumberValue(value["v"]); !ok || version != 1 {
		return 0, false
	}
	var got string
	if raw := value["purpose"]; len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &got) != nil || got != string(purpose) {
		return 0, false
	}
	id, ok := jsNumberValue(value["id"])
	if !ok || math.IsInf(id, 0) || id != math.Trunc(id) || id < 1 {
		return 0, false
	}
	if exp, present := value["exp"]; present {
		seconds, ok := jsNumberValue(exp)
		if !ok || math.IsInf(seconds, 0) || seconds < math.Floor(float64(now.UnixMilli())/1000) {
			return 0, false
		}
	}
	if id > math.MaxInt64 {
		// Number.isInteger accepts it, but no SQLite row id can match it.
		return 0, false
	}
	return int64(id), true
}

// jsNumberValue returns a JSON number as the JavaScript Number JSON.parse
// produces (out-of-range literals become +Infinity or -Infinity); ok is false for any
// other JSON value, including a missing one.
func jsNumberValue(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(raw), 64)
	if err != nil && !math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

// nodeBase64Decode is Buffer.from(value, "base64url"): both the URL-safe and
// the standard alphabet are accepted, other characters are skipped, decoding
// stops at the first "=", and a trailing partial group yields what it can.
func nodeBase64Decode(value string) []byte {
	sextets := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '=':
			i = len(value)
			continue
		case c >= 'A' && c <= 'Z':
			sextets = append(sextets, c-'A')
		case c >= 'a' && c <= 'z':
			sextets = append(sextets, c-'a'+26)
		case c >= '0' && c <= '9':
			sextets = append(sextets, c-'0'+52)
		case c == '+' || c == '-':
			sextets = append(sextets, 62)
		case c == '/' || c == '_':
			sextets = append(sextets, 63)
		}
	}
	decoded := make([]byte, 0, len(sextets)*3/4)
	for len(sextets) >= 4 {
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4, sextets[1]<<4|sextets[2]>>2, sextets[2]<<6|sextets[3])
		sextets = sextets[4:]
	}
	switch len(sextets) {
	case 2:
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4)
	case 3:
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4, sextets[1]<<4|sextets[2]>>2)
	}
	return decoded
}
