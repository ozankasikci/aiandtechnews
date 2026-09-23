package app_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// slowPost sends body in three parts 400ms apart, so it takes about 1.2s.
func slowPost(t *testing.T, server *httptest.Server, target, contentType, authorization, body string) (int, string, error) {
	t.Helper()
	reader, writer := io.Pipe()
	go func() {
		third := len(body) / 3
		for _, chunk := range []string{body[:third], body[third : 2*third], body[2*third:]} {
			time.Sleep(400 * time.Millisecond)
			if _, err := io.WriteString(writer, chunk); err != nil {
				_ = writer.CloseWithError(err)
				return
			}
		}
		_ = writer.Close()
	}()
	request, err := http.NewRequest(http.MethodPost, server.URL+target, reader)
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", contentType)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	return response.StatusCode, string(data), err
}

// The upload route extends its read deadline through the production handler
// (request ID, access log, panic recovery, CORS, chi, auth), so a wrapper that
// hid the connection from http.ResponseController would fail this test. The
// loopback server uses a 300ms ReadTimeout; the login control route on the
// same handler cannot read its slow body.
func TestUploadDeadlineIsExtendedThroughTheProductionHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("slow-body test")
	}
	handler, _, _ := dashboardApplicationWithUploads(t)
	authorization := authorized(t, handler)
	server := httptest.NewUnstartedServer(handler)
	server.Config.ReadTimeout = 300 * time.Millisecond
	server.Config.WriteTimeout = 300 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)

	upload := "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"slow.png\"\r\nContent-Type: image/png\r\n\r\n" + strings.Repeat("s", 3000) + "\r\n--B--\r\n"
	status, body, err := slowPost(t, server, "/api/dashboard/media/upload", "multipart/form-data; boundary=B", authorization, upload)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("slow upload = %d %q, %v", status, body, err)
	}

	login := `{"email":"editorial@example.invalid","password":"` + authTestPassword + `"}` + strings.Repeat(" ", 3000)
	if status, _, err := slowPost(t, server, "/api/auth/login", "application/json", "", login); err == nil && status == http.StatusOK {
		t.Fatal("login read a slow body past the server ReadTimeout; the test cannot prove the extension")
	}
}
