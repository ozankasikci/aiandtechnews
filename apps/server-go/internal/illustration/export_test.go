package illustration

import "net/http"

// CheckReferenceDialAddress exposes the reference client's dial guard.
var CheckReferenceDialAddress = checkReferenceDialAddress

// NewReferenceClientAllowingAnyAddress builds the reference client without the
// dial guard so redirect policy can be tested against a loopback httptest server.
func NewReferenceClientAllowingAnyAddress() *http.Client { return newReferenceClient(nil) }
