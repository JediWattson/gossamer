package browser

import (
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestStorageScopesLocalValuesByOriginAndSessionValuesByPage(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	location, _ := url.Parse("https://strand.test/app")
	first, err := runtime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	firstHost := &taskHost{page: first, generation: first.DocumentGeneration()}
	secondHost := &taskHost{page: second, generation: second.DocumentGeneration()}
	if err := firstHost.StorageSet(LocalStorage, "shared", "yes"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := secondHost.StorageGet(LocalStorage, "shared"); err != nil || !found || value != "yes" {
		t.Fatalf("second-page localStorage = %q, %v, %v", value, found, err)
	}
	if err := firstHost.StorageSet(SessionStorage, "private", "first"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := secondHost.StorageGet(SessionStorage, "private"); err != nil || found || value != "" {
		t.Fatalf("second-page sessionStorage = %q, %v, %v", value, found, err)
	}
	if err := firstHost.StorageSet(LocalStorage, "alpha", "1"); err != nil {
		t.Fatal(err)
	}
	if key, found, err := firstHost.StorageKey(LocalStorage, 0); err != nil || !found || key != "alpha" {
		t.Fatalf("sorted storage key = %q, %v, %v", key, found, err)
	}
	if err := firstHost.StorageSet(LocalStorage, "oversized", strings.Repeat("x", storageQuotaBytes)); err == nil {
		t.Fatal("oversized storage value did not fail")
	}
}

func TestDocumentCookieUsesNavigationLoaderJar(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	location, _ := url.Parse("https://strand.test/app")
	page, err := runtime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	client := loader.New(nil)
	page.SetNavigationLoader(client)
	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	if err := host.SetDocumentCookie("theme=dark; Path=/"); err != nil {
		t.Fatal(err)
	}
	if err := host.SetDocumentCookie("session=strand; Path=/"); err != nil {
		t.Fatal(err)
	}
	value, err := host.DocumentCookie()
	if err != nil {
		t.Fatal(err)
	}
	if value != "theme=dark; session=strand" {
		t.Fatalf("document.cookie = %q", value)
	}
	cookies := client.Cookies(location)
	if len(cookies) != 2 {
		t.Fatalf("loader cookies = %#v", cookies)
	}
}
