package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSelectEngineSupportsStrand(t *testing.T) {
	engine, err := selectEngine("strand", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectEngineRejectsUnknownName(t *testing.T) {
	if _, err := selectEngine("spark", ""); err == nil {
		t.Fatal("selectEngine accepted an unknown engine")
	}
}

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (fn stubRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestHTTPTraceTransportLogsResponseAndError(t *testing.T) {
	var buf bytes.Buffer
	okTransport := httpTraceTransport{
		base: stubRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
		out: &buf,
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.test/api/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := okTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := buf.String(); !strings.Contains(got, "[http] POST https://example.test/api/items -> 201") {
		t.Fatalf("trace output = %q", got)
	}

	buf.Reset()
	errTransport := httpTraceTransport{
		base: stubRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		}),
		out: &buf,
	}
	if _, err := errTransport.RoundTrip(req); err == nil {
		t.Fatal("expected error from RoundTrip")
	}
	if got := buf.String(); !strings.Contains(got, "[http] POST https://example.test/api/items -> error") || !strings.Contains(got, "dial failed") {
		t.Fatalf("error trace output = %q", got)
	}
}
