package newsletter

import (
	"testing"
	"time"
)

// Existing confirm and unsubscribe links in sent emails were signed by Node;
// Go must produce and accept exactly the same tokens.
func TestCreateTokenMatchesNodeByteForByte(t *testing.T) {
	creates := golden(t).Tokens.Creates
	if len(creates) != 21 {
		t.Fatalf("recorded token vectors = %d", len(creates))
	}
	for _, vector := range creates {
		var expiresAt *time.Time
		if vector.ExpiresAt != nil {
			parsed, err := time.Parse(time.RFC3339Nano, *vector.ExpiresAt)
			if err != nil {
				t.Fatal(err)
			}
			expiresAt = &parsed
		}
		got := CreateToken(vector.ID, Purpose(vector.Purpose), vector.Secret, expiresAt)
		if got != vector.Token {
			t.Errorf("CreateToken(%d, %s, exp %v) = %s, want %s", vector.ID, vector.Purpose, vector.ExpiresAt, got, vector.Token)
		}
	}
}

func TestVerifyTokenMatchesNode(t *testing.T) {
	verifies := golden(t).Tokens.Verifies
	if len(verifies) < 40 {
		t.Fatalf("recorded verification vectors = %d", len(verifies))
	}
	for _, vector := range verifies {
		now, err := time.Parse(time.RFC3339Nano, vector.Now)
		if err != nil {
			t.Fatal(err)
		}
		id, ok := VerifyToken(vector.Token, Purpose(vector.Purpose), vector.Secret, now)
		switch {
		case vector.Result == nil && ok:
			t.Errorf("%s: VerifyToken = %d, want null", vector.Name, id)
		case vector.Result != nil && (!ok || id != *vector.Result):
			t.Errorf("%s: VerifyToken = %d, %t, want %d", vector.Name, id, ok, *vector.Result)
		}
	}
}

func TestNodeBase64DecodingIsLenientLikeBuffer(t *testing.T) {
	// Recorded with Buffer.from(value, "base64url").toString("latin1").
	for input, want := range map[string]string{
		"YWJj": "abc", "YWJjZA": "abcd", "YWJjZA==": "abcd", "YWJjZA=": "abcd", "YW Jj": "abc",
		"YW!Jj": "abc", "YWJj=ZGVm": "abc", "YW=Jj": "a", "Y": "", "YW": "a", "+/+/": "\xfb\xff\xbf",
		"-_-_": "\xfb\xff\xbf", "YWJ\njZA": "abcd", "YWJjZA===": "abcd", "=YWJj": "", "YWJjZ": "abc", "YWJ*j": "abc",
	} {
		if got := string(nodeBase64Decode(input)); got != want {
			t.Errorf("nodeBase64Decode(%q) = %q, want %q", input, got, want)
		}
	}
}
