package html

import (
	"errors"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestParseFragmentBuildsDetachedChildren(t *testing.T) {
	fragment, err := ParseFragment(strings.NewReader(`<span class="one">alpha</span><!--gap--><strong>omega</strong>`), "div")
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Type != dom.DocumentFragmentNode || fragment.Parent != nil || len(fragment.Children) != 3 {
		t.Fatalf("fragment = %#v", fragment)
	}
	if fragment.Children[0].Data != "span" || fragment.Children[0].Parent != fragment ||
		fragment.Children[2].Data != "strong" || fragment.Children[2].Children[0].Data != "omega" {
		t.Fatalf("fragment children = %#v", fragment.Children)
	}
	if got := SerializeChildren(fragment); got != `<span class="one">alpha</span><!--gap--><strong>omega</strong>` {
		t.Fatalf("SerializeChildren = %q", got)
	}
}

func TestTemplateChildrenUseInertContentFragment(t *testing.T) {
	document, err := Parse(strings.NewReader(`<template id="card"><article><span>inside</span></article></template><p>outside</p>`))
	if err != nil {
		t.Fatal(err)
	}
	indexed, err := dom.IndexDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	templateID, found := indexed.ElementByID("card")
	if !found {
		t.Fatal("template element is missing")
	}
	if children, err := indexed.ChildNodes(templateID, false); err != nil || len(children) != 0 {
		t.Fatalf("template light children = %#v, %v", children, err)
	}
	contentID, err := indexed.TemplateContent(templateID)
	if err != nil {
		t.Fatal(err)
	}
	children, err := indexed.ChildNodes(contentID, false)
	if err != nil || len(children) != 1 {
		t.Fatalf("template content children = %#v, %v", children, err)
	}
	article, _ := indexed.Snapshot(children[0])
	if article.Data != "article" || article.Connected {
		t.Fatalf("inert article snapshot = %#v", article)
	}
	if got := SerializeNode(document); got != `<html><head></head><body><template id="card"><article><span>inside</span></article></template><p>outside</p></body></html>` {
		t.Fatalf("serialized template document = %q", got)
	}
}

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

func TestParseTableModesInsertMissingGroupsRowsAndCells(t *testing.T) {
	t.Parallel()

	input := `<table id=t><td>A<td>B<tr><th>C</table><p>after`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <table id=\"t\">\n" +
		"        <tbody>\n" +
		"          <tr>\n" +
		"            <td>\n" +
		"              #text \"A\"\n" +
		"            <td>\n" +
		"              #text \"B\"\n" +
		"          <tr>\n" +
		"            <th>\n" +
		"              #text \"C\"\n" +
		"      <p>\n" +
		"        #text \"after\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseTableModesFosterMisnestedContentBeforeTable(t *testing.T) {
	t.Parallel()

	input := `<div>before</div><table>alpha<div>inside</div><tr><td>cell</td></tr>omega</table><p>after</p>`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <div>\n" +
		"        #text \"before\"\n" +
		"      #text \"alpha\"\n" +
		"      <div>\n" +
		"        #text \"inside\"\n" +
		"      #text \"omega\"\n" +
		"      <table>\n" +
		"        <tbody>\n" +
		"          <tr>\n" +
		"            <td>\n" +
		"              #text \"cell\"\n" +
		"      <p>\n" +
		"        #text \"after\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseTableModesHandleCaptionColumnsSectionsAndNestedTable(t *testing.T) {
	t.Parallel()

	input := `<table><caption>Cap<col span=2><thead><tr><th>H<tbody><tr><td>A<table><tr><td>N</table><tfoot><tr><td>F</table>`
	want := "" +
		"#document\n" +
		"  <html>\n" +
		"    <head>\n" +
		"    <body>\n" +
		"      <table>\n" +
		"        <caption>\n" +
		"          #text \"Cap\"\n" +
		"        <colgroup>\n" +
		"          <col span=\"2\">\n" +
		"        <thead>\n" +
		"          <tr>\n" +
		"            <th>\n" +
		"              #text \"H\"\n" +
		"        <tbody>\n" +
		"          <tr>\n" +
		"            <td>\n" +
		"              #text \"A\"\n" +
		"              <table>\n" +
		"                <tbody>\n" +
		"                  <tr>\n" +
		"                    <td>\n" +
		"                      #text \"N\"\n" +
		"        <tfoot>\n" +
		"          <tr>\n" +
		"            <td>\n" +
		"              #text \"F\"\n"

	if got := parseAndDump(t, strings.NewReader(input)); got != want {
		t.Errorf("DOM:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseTableFragmentUsesContextInsertionMode(t *testing.T) {
	t.Parallel()

	fragment, err := ParseFragment(strings.NewReader(`<td>A<td>B`), "table")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := SerializeChildren(fragment), `<tbody><tr><td>A</td><td>B</td></tr></tbody>`; got != want {
		t.Fatalf("table fragment = %q, want %q", got, want)
	}
}

func TestParseTableTextAtEOFIsFosterParented(t *testing.T) {
	t.Parallel()

	if got, want := SerializeNode(mustParseHTML(t, `<table>tail`)), `<html><head></head><body>tail<table></table></body></html>`; got != want {
		t.Fatalf("EOF table text = %q, want %q", got, want)
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

func mustParseHTML(t *testing.T, source string) *dom.Node {
	t.Helper()
	document, err := Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return document
}
