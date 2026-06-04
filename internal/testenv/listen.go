package testenv

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
)

// CanListenTCP4 reports whether the current environment permits opening a
// loopback TCP listener.
func CanListenTCP4() error {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	return ln.Close()
}

// CanStartHTTPServer reports whether httptest can bootstrap a local HTTP
// server. httptest.NewServer panics on listen failure, so recover and surface
// the cause as an error that tests can turn into Skip.
func CanStartHTTPServer() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("httptest.NewServer failed: %v", r)
		}
	}()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return nil
}
