package webapi_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/webapi"
)

func TestURLResolutionComponentsAndMutation(t *testing.T) {
	base := "https://user:pass@example.test:8443/app/index.html?old=1#start"
	value, err := webapi.ParseURL("../chat?room=one+two#latest", &base)
	if err != nil {
		t.Fatal(err)
	}
	if value.Origin() != "https://example.test:8443" || value.Pathname() != "/chat" ||
		value.Search() != "?room=one+two" || value.Hash() != "#latest" || value.Hostname() != "example.test" {
		t.Fatalf("URL components = %#v", value)
	}
	value.SetSearch("?room=next")
	value.SetHash("#done")
	if got := value.String(); got != "https://user:pass@example.test:8443/chat?room=next#done" {
		t.Fatalf("mutated URL = %q", got)
	}
}
