package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>OMM</title>")},
		"assets/app.js":        {Data: []byte("console.log('omm')")},
		"manifest.webmanifest": {Data: []byte(`{"name":"OMM"}`)},
	}
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	return rw
}

func TestHandlerServesIndexAtRoot(t *testing.T) {
	rw := doGet(t, NewHandler(testFS()), "/")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	body, _ := io.ReadAll(rw.Body)
	if want := "<!doctype html>"; string(body[:len(want)]) != want {
		t.Fatalf("expected index.html body, got %q", string(body))
	}
	if ct := rw.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("expected a Content-Type header")
	}
}

func TestHandlerServesStaticAsset(t *testing.T) {
	rw := doGet(t, NewHandler(testFS()), "/assets/app.js")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	body, _ := io.ReadAll(rw.Body)
	if string(body) != "console.log('omm')" {
		t.Fatalf("unexpected asset body: %q", string(body))
	}
}

func TestHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	// A deep-link to a client-side route that is not a real file must serve
	// index.html so the SPA router can take over.
	rw := doGet(t, NewHandler(testFS()), "/some/client/route")
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200 fallback, got %d", rw.Code)
	}
	body, _ := io.ReadAll(rw.Body)
	if want := "<!doctype html>"; string(body[:len(want)]) != want {
		t.Fatalf("expected index.html fallback, got %q", string(body))
	}
}

func TestHandlerReturns404WhenIndexMissing(t *testing.T) {
	rw := doGet(t, NewHandler(fstest.MapFS{}), "/")
	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no index.html, got %d", rw.Code)
	}
}
