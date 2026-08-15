package html

import (
	"errors"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestParseBuildsImplicitDocumentStructure(t *testing.T) {
	t.Parallel()

	input := `<!doctype html><title>Gossamer &amp; Go</title><p class=x>Hello<br>world`
	want := "" +
		"#document\n" +
		"  #doctype \"html\"\n" +
		"  <html>\n" +
		"    <head>\n" +
		"      <title>\n" +
		"        #text \"Gossamer & Go\"\n" +
		"    <body>\n" +
		"      <p class=\"x\">\n" +
		"        #text \"Hello\"\n" +
		"        <br>\n" +
		"        #text \"world\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseEmptyDocumentCreatesRootElements(t *testing.T) {
	t.Parallel()

	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n"
	if got := parseAndDump(t, strings.NewReader("")); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseAppliesBasicImpliedEndTags(t *testing.T) {
	t.Parallel()

	input := `<p>One<p>Two<ul><li>First<li>Second</ul>`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <p>\n" +
		"        #text \"One\"\n" +
		"      <p>\n" +
		"        #text \"Two\"\n" +
		"      <ul>\n" +
		"        <li>\n" +
		"          #text \"First\"\n" +
		"        <li>\n" +
		"          #text \"Second\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseKeepsNestedListItemsInTheirNearestList(t *testing.T) {
	t.Parallel()

	input := `<ul><li>outer<ul><li>inner<li>second</ul><li>outer second</ul>`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <ul>\n" +
		"        <li>\n" +
		"          #text \"outer\"\n" +
		"          <ul>\n" +
		"            <li>\n" +
		"              #text \"inner\"\n" +
		"            <li>\n" +
		"              #text \"second\"\n" +
		"        <li>\n" +
		"          #text \"outer second\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseIgnoresSelfClosingFlagOnNormalHTMLElement(t *testing.T) {
	t.Parallel()

	input := `<div/>inside<br/>after`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <div>\n" +
		"        #text \"inside\"\n" +
		"        <br>\n" +
		"        #text \"after\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseTextElementModes(t *testing.T) {
	t.Parallel()

	input := `<title>a&amp;b</title><style>a&amp;b</style><body><textarea>
X&amp;Y</textarea><script>if (a < b) {}</script>`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"      <title>\n" +
		"        #text \"a&b\"\n" +
		"      <style>\n" +
		"        #text \"a&amp;b\"\n" +
		"    <body>\n" +
		"      <textarea>\n" +
		"        #text \"X&Y\"\n" +
		"      <script>\n" +
		"        #text \"if (a < b) {}\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseProcessingInstruction(t *testing.T) {
	t.Parallel()

	input := `<?build debug?><main><?render fast?></main>`
	want := "" +
		"#document\n" +
		"  #processing-instruction \"build\" \"debug\"\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <main>\n" +
		"        #processing-instruction \"render\" \"fast\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseIsIndependentOfReaderChunking(t *testing.T) {
	t.Parallel()

	input := "<!doctype html><html lang=en><head><title>Snowman ☃</title></head><body><p>A&amp;B<!-- note --></p></body></html>"
	want := parseAndDump(t, strings.NewReader(input))
	got := parseAndDump(t, oneByteReader{reader: strings.NewReader(input)})
	if got != want {
		t.Errorf("one-byte DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseReturnsReaderError(t *testing.T) {
	t.Parallel()

	want := errors.New("read failed")
	document, err := Parse(errorReader{err: want})
	if document != nil {
		t.Errorf("Parse() document = %v, want nil", document)
	}
	if !errors.Is(err, want) {
		t.Errorf("Parse() error = %v, want %v", err, want)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"",
		"<p>hello",
		"<title>a&amp;b</title>",
		"<table><tr><td>cell",
		"<?build debug?>",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		document, err := Parse(oneByteReader{reader: strings.NewReader(input)})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if err := dom.Dump(&strings.Builder{}, document); err != nil {
			t.Fatalf("Dump() error = %v", err)
		}
	})
}

func parseAndDump(t *testing.T, reader interface{ Read([]byte) (int, error) }) string {
	t.Helper()

	document, err := Parse(reader)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var output strings.Builder
	if err := dom.Dump(&output, document); err != nil {
		t.Fatalf("Dump() error = %v", err)
	}
	return output.String()
}
