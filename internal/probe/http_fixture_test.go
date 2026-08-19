package probe

import (
	"errors"
	"io"
	"net/http"
)

type fixtureRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip fixtureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func fixtureResponse(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: body, Header: make(http.Header)}
}

type fixtureReadError struct{}

func (fixtureReadError) Read([]byte) (int, error) { return 0, errors.New("fixture read failure") }
func (fixtureReadError) Close() error             { return nil }
