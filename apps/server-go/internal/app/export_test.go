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
