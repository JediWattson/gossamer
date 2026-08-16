package browser

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/resource"
)

func TestLoadStylesheetWithImportsFlattensOrderContextAndCycles(t *testing.T) {
	t.Parallel()

	fetcher := &importMapFetcher{
		responses: map[string]string{
			"https://imports.test/root.css": `
				@import "base.css";
				@import "theme.css" layer(theme) supports((display: block) and selector(.theme)) screen and (min-width: 700px);
				@import "unsupported.css" supports((display: grid) or selector(div::before));
				@import "cycle.css";
				.root { color: black }
			`,
			"https://imports.test/base.css": `
				@import "nested.css";
				.base { color: red }
			`,
			"https://imports.test/nested.css":      `.nested { color: green }`,
			"https://imports.test/theme.css":       `@layer inner { .theme { color: blue } }`,
			"https://imports.test/cycle.css":       `@import "root.css"; .cycle { color: gray }`,
			"https://imports.test/unsupported.css": `.unsupported { color: white }`,
		},
		calls: make(map[string]int),
	}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	rootURL, _ := url.Parse("https://imports.test/root.css")
	asset, err := pipeline.Fetch(context.Background(), resource.Reference{Kind: resource.Stylesheet, URL: rootURL})
	if err != nil {
		t.Fatal(err)
	}
	stylesheet, err := loadStylesheetWithImports(context.Background(), pipeline, asset)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.Rules), 5; got != want {
		t.Fatalf("rules = %d, want %d", got, want)
	}
	wantClasses := []string{"nested", "base", "theme", "cycle", "root"}
	for index, want := range wantClasses {
		node := testClassNode(want)
		if _, matched := stylesheet.Rules[index].Match(node); !matched {
			t.Fatalf("rule %d does not match .%s", index, want)
		}
		if stylesheet.Rules[index].Order != index {
			t.Fatalf("rule %d order = %d", index, stylesheet.Rules[index].Order)
		}
	}
	if got := stylesheet.Rules[2].Layer; got != "theme.inner" {
		t.Fatalf("theme imported layer = %q", got)
	}
	if got, want := stylesheet.Rules[2].Media, []string{"screen and (min-width: 700px)"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("theme media = %v, want %v", got, want)
	}
	if stylesheet.Rules[2].MatchesMedia(css.MediaEnvironment{Type: "screen", Width: 640, Height: 480}) {
		t.Fatal("imported media context matched a narrow viewport")
	}
	if !stylesheet.Rules[2].MatchesMedia(css.MediaEnvironment{Type: "screen", Width: 800, Height: 600}) {
		t.Fatal("imported media context did not match a wide viewport")
	}
	if fetcher.calls["https://imports.test/root.css"] != 1 {
		t.Fatalf("root fetch count = %d, want 1 despite cycle", fetcher.calls["https://imports.test/root.css"])
	}
	if fetcher.calls["https://imports.test/unsupported.css"] != 0 {
		t.Fatal("false @supports import was fetched")
	}
}

func TestLoadStylesheetWithImportsBoundsDepth(t *testing.T) {
	t.Parallel()

	responses := make(map[string]string)
	for index := 0; index < maxStylesheetImportDepth+4; index++ {
		next := index + 1
		responses["https://depth.test/"+itoa(index)+".css"] = `@import "` + itoa(next) + `.css"; .d` + itoa(index) + ` { color: red }`
	}
	fetcher := &importMapFetcher{responses: responses, calls: make(map[string]int)}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	rootURL, _ := url.Parse("https://depth.test/0.css")
	asset, err := pipeline.Fetch(context.Background(), resource.Reference{Kind: resource.Stylesheet, URL: rootURL})
	if err != nil {
		t.Fatal(err)
	}
	stylesheet, err := loadStylesheetWithImports(context.Background(), pipeline, asset)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(stylesheet.Rules); got != maxStylesheetImportDepth+1 {
		t.Fatalf("bounded rules = %d, want %d", got, maxStylesheetImportDepth+1)
	}
	if fetcher.calls["https://depth.test/17.css"] != 0 {
		t.Fatal("loader fetched beyond the import depth bound")
	}
}

type importMapFetcher struct {
	responses map[string]string
	calls     map[string]int
}

func (fetcher *importMapFetcher) LoadResource(_ context.Context, rawURL string, _ loader.Destination) (*loader.Response, error) {
	fetcher.calls[rawURL]++
	body, ok := fetcher.responses[rawURL]
	if !ok {
		body = ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "text/css")
	return &loader.Response{
		URL: parsed, StatusCode: http.StatusOK, Header: header,
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

func testClassNode(class string) *dom.Node {
	return dom.NewElement("div", dom.Attribute{Name: "class", Value: class})
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
