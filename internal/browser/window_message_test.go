package browser

import (
	"net/url"
	"testing"
)

func TestSerializedWindowOriginNormalizesDefaultPortsAndIPv6(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://EXAMPLE.test:443/path": "https://example.test",
		"http://EXAMPLE.test:8080/path": "http://example.test:8080",
		"https://[::1]/path":            "https://[::1]",
		"https://[::1]:8443/path":       "https://[::1]:8443",
		"file:///tmp/example":           "null",
	}
	for raw, want := range tests {
		location, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := serializedWindowOrigin(location); got != want {
			t.Errorf("serializedWindowOrigin(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWindowTargetOriginRequiresAnOrigin(t *testing.T) {
	t.Parallel()

	tests := []string{
		"https://child.test/path",
		"https://child.test/?query=1",
		"https://user@child.test/",
		"javascript:alert(1)",
	}
	for _, target := range tests {
		if _, err := acceptsWindowTargetOrigin(target, "https://parent.test", "https://child.test"); err == nil {
			t.Errorf("acceptsWindowTargetOrigin(%q) accepted a non-origin URL", target)
		}
	}
	if accepted, err := acceptsWindowTargetOrigin("https://CHILD.test:443/", "https://parent.test", "https://child.test"); err != nil || !accepted {
		t.Fatalf("normalized exact target origin = %t, %v", accepted, err)
	}
}
