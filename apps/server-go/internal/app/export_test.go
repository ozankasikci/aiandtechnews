package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// StubObjectStorageForTest replaces the AWS client factory and the HTTP client
// used for public-URL verification, so external tests stay offline.
func StubObjectStorageForTest(t testing.TB, open func(context.Context, config.Config) (media.ObjectAPI, error), client *http.Client) {
	t.Helper()
	originalOpen, originalClient := openPublisherStorage, newPublicHTTPClient
	openPublisherStorage = open
	newPublicHTTPClient = func() *http.Client { return client }
	t.Cleanup(func() { openPublisherStorage, newPublicHTTPClient = originalOpen, originalClient })
}

// StubNewsletterDeliveryForTest points newsletter delivery at a test server
// (an httptest Resend stand-in) and replaces the 550ms pause between digest
// deliveries, so app tests never reach the real Resend API or wait.
func StubNewsletterDeliveryForTest(t testing.TB, endpoint string, client *http.Client, pace func(context.Context) error) {
	t.Helper()
	originalEndpoint, originalClient, originalPace := resendEndpoint, newResendHTTPClient, newsletterPace
	resendEndpoint = endpoint
	newResendHTTPClient = func() *http.Client { return client }
	newsletterPace = pace
	t.Cleanup(func() {
		resendEndpoint, newResendHTTPClient, newsletterPace = originalEndpoint, originalClient, originalPace
	})
}
