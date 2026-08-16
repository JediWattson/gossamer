package style

import (
	"strconv"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestProvenancePresentationalHintRollbackPointerAndStable(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleOwner := dom.NewElement("style")
	styleOwner.AppendChild(dom.NewText(`#target { all: revert-layer; }`))
	head.AppendChild(styleOwner)
	body := dom.NewElement("body")
	target := dom.NewElement("img",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "width", Value: "41"},
	)
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment: Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
	})
	styleOwnerID := provenanceNodeID(t, document, styleOwner)
	targetID := provenanceNodeID(t, document, target)

	pointerExplanation := requirePointerExplanation(t, pointer, target, "width")
	assertHintRollbackExplanation(t, pointerExplanation)
	if pointerExplanation.Controller.owner != styleOwner || pointerExplanation.Controller.OwnerID != dom.InvalidNodeID {
		t.Errorf("pointer controller owner = %p/%d, want style owner %p/0", pointerExplanation.Controller.owner, pointerExplanation.Controller.OwnerID, styleOwner)
	}
	if pointerExplanation.ValueSource.owner != target || pointerExplanation.ValueSource.OwnerID != dom.InvalidNodeID {
		t.Errorf("pointer value source owner = %p/%d, want target %p/0", pointerExplanation.ValueSource.owner, pointerExplanation.ValueSource.OwnerID, target)
	}

	stableExplanation := requireStableExplanation(t, stable, targetID, "WIDTH")
	assertHintRollbackExplanation(t, stableExplanation)
	assertStableOwner(t, "hint rollback controller", stableExplanation.Controller, styleOwnerID)
	assertStableOwner(t, "hint rollback value source", stableExplanation.ValueSource, targetID)
	assertStableProvenanceHasNoPointers(t, stable)

	pointerDump := pointer.Dump(target)
	stableDump := stable.DumpID(targetID)
	assertOrdinaryDumpOrder(t, pointerDump)
	assertOrdinaryDumpOrder(t, stableDump)
	if pointer.Dump(target) != pointerDump || stable.DumpID(targetID) != stableDump {
		t.Fatal("repeated provenance dumps were not deterministic")
	}

	if err := document.SetAttribute(targetID, "width", "82"); err != nil {
		t.Fatal(err)
	}
	if got := requirePointerExplanation(t, pointer, target, "width"); got.Value != "41px" || got.ValueSource.DeclarationValue != "41" {
		t.Errorf("pointer explanation changed after DOM mutation: %#v", got)
	}
	if got := requireStableExplanation(t, stable, targetID, "width"); got.Value != "41px" || got.ValueSource.DeclarationValue != "41" {
		t.Errorf("stable explanation changed after DOM mutation: %#v", got)
	}
	if got := pointer.Dump(target); got != pointerDump {
		t.Error("pointer Dump changed after DOM mutation")
	}
	if got := stable.DumpID(targetID); got != stableDump {
		t.Error("stable DumpID changed after DOM mutation")
	}
}

func TestProvenanceChainedCustomRollbackPointerAndStable(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleOwner := dom.NewElement("style")
	authorSource := `
		@layer base, theme;
		@layer base { #target { --tone: revert; } }
		@layer theme { #target { --tone: revert-layer; } }
	`
	styleOwner.AppendChild(dom.NewText(authorSource))
	head.AppendChild(styleOwner)
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	userSource := `#target { --tone: from-user; }`
	user, err := css.Parse(userSource)
	if err != nil {
		t.Fatal(err)
	}

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment:     Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
		UserStylesheets: []css.Stylesheet{user},
	})
	styleOwnerID := provenanceNodeID(t, document, styleOwner)
	targetID := provenanceNodeID(t, document, target)

	pointerExplanation := requirePointerExplanation(t, pointer, target, "--tone")
	assertCustomRollbackExplanation(t, pointerExplanation)
	if pointerExplanation.Controller.owner != styleOwner || pointerExplanation.Rollback[0].owner != styleOwner {
		t.Errorf("pointer rollback owners = controller:%p rollback:%p, want %p", pointerExplanation.Controller.owner, pointerExplanation.Rollback[0].owner, styleOwner)
	}

	stableExplanation := requireStableExplanation(t, stable, targetID, "--tone")
	assertCustomRollbackExplanation(t, stableExplanation)
	assertStableOwner(t, "custom rollback controller", stableExplanation.Controller, styleOwnerID)
	assertStableOwner(t, "custom rollback step", stableExplanation.Rollback[0], styleOwnerID)
	assertPropertySourceSpans(t, "custom rollback controller", authorSource, stableExplanation.Controller,
		`--tone: revert-layer`, `--tone`, `revert-layer`)
	assertPropertySourceSpans(t, "custom rollback step", authorSource, stableExplanation.Rollback[0],
		`--tone: revert`, `--tone`, `revert`)
	assertPropertySourceSpans(t, "custom rollback value", userSource, stableExplanation.ValueSource,
		`--tone: from-user`, `--tone`, `from-user`)
	if stableExplanation.ValueSource.OwnerID != dom.InvalidNodeID || stableExplanation.ValueSource.owner != nil {
		t.Errorf("user stylesheet value source owner = %d/%p, want no element owner", stableExplanation.ValueSource.OwnerID, stableExplanation.ValueSource.owner)
	}

	pointerDump := pointer.Dump(target)
	stableDump := stable.DumpID(targetID)
	assertCustomAfterOrdinaryDump(t, pointerDump, "--tone")
	assertCustomAfterOrdinaryDump(t, stableDump, "--tone")
}

func TestProvenanceCustomRollbackRetainsExposedInheritController(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleOwner := dom.NewElement("style")
	styleOwner.AppendChild(dom.NewText(`#target { --tone: revert; }`))
	head.AppendChild(styleOwner)
	parent := dom.NewElement("body", dom.Attribute{Name: "style", Value: "--tone: #123456"})
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	parent.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(parent)
	root.AppendChild(html)
	user, err := css.Parse(`#target { --tone: inherit; }`)
	if err != nil {
		t.Fatal(err)
	}

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment:     Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
		UserStylesheets: []css.Stylesheet{user},
	})
	styleOwnerID := provenanceNodeID(t, document, styleOwner)
	parentID := provenanceNodeID(t, document, parent)
	targetID := provenanceNodeID(t, document, target)

	for name, explanation := range map[string]PropertyExplanation{
		"pointer": requirePointerExplanation(t, pointer, target, "--tone"),
		"stable":  requireStableExplanation(t, stable, targetID, "--tone"),
	} {
		if explanation.Value != "#123456" || explanation.Resolution != ResolutionRevert {
			t.Errorf("%s exposed-inherit value/resolution = %q/%s", name, explanation.Value, explanation.Resolution)
		}
		if explanation.Controller.Origin != CascadeOriginAuthor || explanation.Controller.DeclarationValue != "revert" {
			t.Errorf("%s exposed-inherit controller = %#v, want author revert", name, explanation.Controller)
		}
		if len(explanation.Rollback) != 1 || explanation.Rollback[0].Origin != CascadeOriginUser || explanation.Rollback[0].DeclarationValue != "inherit" {
			t.Errorf("%s exposed-inherit rollback = %#v, want user inherit", name, explanation.Rollback)
		}
		if explanation.ValueSource.Kind != SourceInlineStyle || explanation.ValueSource.DeclarationValue != "#123456" {
			t.Errorf("%s exposed-inherit value source = %#v, want parent inline value", name, explanation.ValueSource)
		}
	}
	pointerExplanation := requirePointerExplanation(t, pointer, target, "--tone")
	if pointerExplanation.Controller.owner != styleOwner || pointerExplanation.Rollback[0].owner != nil || pointerExplanation.ValueSource.owner != parent {
		t.Errorf("pointer exposed-inherit owners = %p/%p/%p, want %p/nil/%p", pointerExplanation.Controller.owner, pointerExplanation.Rollback[0].owner, pointerExplanation.ValueSource.owner, styleOwner, parent)
	}
	stableExplanation := requireStableExplanation(t, stable, targetID, "--tone")
	assertStableOwner(t, "exposed-inherit controller", stableExplanation.Controller, styleOwnerID)
	if stableExplanation.Rollback[0].OwnerID != dom.InvalidNodeID || stableExplanation.Rollback[0].owner != nil {
		t.Errorf("stable user inherit owner = %d/%p, want no element owner", stableExplanation.Rollback[0].OwnerID, stableExplanation.Rollback[0].owner)
	}
	assertStableOwner(t, "exposed-inherit value source", stableExplanation.ValueSource, parentID)
}

func TestProvenanceInvalidComputedValueDoesNotExposeLosingCandidate(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleOwner := dom.NewElement("style")
	styleOwner.AppendChild(dom.NewText(`
		#target { color: #010203; }
		#target { color: var(--missing); }
	`))
	head.AppendChild(styleOwner)
	parent := dom.NewElement("body", dom.Attribute{Name: "style", Value: "color: #112233"})
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	parent.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(parent)
	root.AppendChild(html)

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment: Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
	})
	styleOwnerID := provenanceNodeID(t, document, styleOwner)
	parentID := provenanceNodeID(t, document, parent)
	targetID := provenanceNodeID(t, document, target)

	for name, explanation := range map[string]PropertyExplanation{
		"pointer": requirePointerExplanation(t, pointer, target, "color"),
		"stable":  requireStableExplanation(t, stable, targetID, "color"),
	} {
		if explanation.Value != "rgb(17, 34, 51)" || explanation.Resolution != ResolutionInvalidAtComputedValue {
			t.Errorf("%s invalid-var value/resolution = %q/%s", name, explanation.Value, explanation.Resolution)
		}
		if explanation.Controller.DeclarationValue != "var(--missing)" || explanation.Controller.Kind != SourceStylesheet {
			t.Errorf("%s invalid-var controller = %#v", name, explanation.Controller)
		}
		if explanation.ValueSource.DeclarationValue != "#112233" || explanation.ValueSource.Kind != SourceInlineStyle {
			t.Errorf("%s invalid-var value source = %#v, want parent inline declaration", name, explanation.ValueSource)
		}
		if explanation.ValueSource.DeclarationValue == "#010203" || len(explanation.Rollback) != 0 {
			t.Errorf("%s invalid-var resurrected losing candidate: %#v", name, explanation)
		}
	}
	pointerExplanation := requirePointerExplanation(t, pointer, target, "color")
	if pointerExplanation.Controller.owner != styleOwner || pointerExplanation.ValueSource.owner != parent {
		t.Errorf("pointer invalid-var owners = %p/%p, want %p/%p", pointerExplanation.Controller.owner, pointerExplanation.ValueSource.owner, styleOwner, parent)
	}
	stableExplanation := requireStableExplanation(t, stable, targetID, "color")
	assertStableOwner(t, "invalid-var controller", stableExplanation.Controller, styleOwnerID)
	assertStableOwner(t, "invalid-var value source", stableExplanation.ValueSource, parentID)
	if strings.Contains(pointer.Dump(target), `authored-value="#010203"`) || strings.Contains(stable.DumpID(targetID), `authored-value="#010203"`) {
		t.Error("computed provenance dump exposed the losing lower declaration")
	}
}

func TestProvenanceNaturalAndExplicitInheritanceUseUltimateParentSource(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: "color: #123456"})
	natural := dom.NewElement("span", dom.Attribute{Name: "id", Value: "natural"})
	explicit := dom.NewElement("span",
		dom.Attribute{Name: "id", Value: "explicit"},
		dom.Attribute{Name: "style", Value: "color: inherit"},
	)
	parent.AppendChild(natural)
	parent.AppendChild(explicit)
	root.AppendChild(parent)

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment: Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
	})
	parentID := provenanceNodeID(t, document, parent)
	naturalID := provenanceNodeID(t, document, natural)
	explicitID := provenanceNodeID(t, document, explicit)

	pointerNatural := requirePointerExplanation(t, pointer, natural, "color")
	assertInheritedValueSource(t, "pointer natural", pointerNatural)
	if pointerNatural.Resolution != ResolutionInherited || pointerNatural.Controller.Kind != SourceInherited || pointerNatural.Controller.owner != parent {
		t.Errorf("pointer natural controller = %#v/%s, want inherited parent", pointerNatural.Controller, pointerNatural.Resolution)
	}
	pointerExplicit := requirePointerExplanation(t, pointer, explicit, "color")
	assertInheritedValueSource(t, "pointer explicit", pointerExplicit)
	if pointerExplicit.Resolution != ResolutionInherited || pointerExplicit.Controller.Kind != SourceInlineStyle || pointerExplicit.Controller.DeclarationValue != "inherit" || pointerExplicit.Controller.owner != explicit {
		t.Errorf("pointer explicit controller = %#v/%s, want inline inherit", pointerExplicit.Controller, pointerExplicit.Resolution)
	}

	stableNatural := requireStableExplanation(t, stable, naturalID, "COLOR")
	assertInheritedValueSource(t, "stable natural", stableNatural)
	if stableNatural.Resolution != ResolutionInherited || stableNatural.Controller.Kind != SourceInherited {
		t.Errorf("stable natural controller = %#v/%s, want inherited", stableNatural.Controller, stableNatural.Resolution)
	}
	assertStableOwner(t, "natural inherited controller", stableNatural.Controller, parentID)
	assertStableOwner(t, "natural inherited value source", stableNatural.ValueSource, parentID)

	stableExplicit := requireStableExplanation(t, stable, explicitID, "color")
	assertInheritedValueSource(t, "stable explicit", stableExplicit)
	if stableExplicit.Resolution != ResolutionInherited || stableExplicit.Controller.Kind != SourceInlineStyle || stableExplicit.Controller.DeclarationValue != "inherit" {
		t.Errorf("stable explicit controller = %#v/%s, want inline inherit", stableExplicit.Controller, stableExplicit.Resolution)
	}
	assertStableOwner(t, "explicit inherited controller", stableExplicit.Controller, explicitID)
	assertStableOwner(t, "explicit inherited value source", stableExplicit.ValueSource, parentID)
}

func TestProvenanceRetainsStylesheetAndInlineDeclarationSpans(t *testing.T) {
	t.Parallel()

	stylesheetSource := `/* lead */ #target { background-color: #010203; }`
	inlineSource := ` width: 20px !important; `
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleOwner := dom.NewElement("style")
	styleOwner.AppendChild(dom.NewText(stylesheetSource))
	head.AppendChild(styleOwner)
	body := dom.NewElement("body")
	target := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "style", Value: inlineSource},
	)
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)

	pointer, stable, document := computeProvenanceSnapshots(t, root, Input{
		Environment: Environment{Width: 320, Height: 200, MediaType: "screen", InitialFontSize: 16},
	})
	targetID := provenanceNodeID(t, document, target)

	for name, explanation := range map[string]PropertyExplanation{
		"pointer stylesheet": requirePointerExplanation(t, pointer, target, "background-color"),
		"stable stylesheet":  requireStableExplanation(t, stable, targetID, "background-color"),
	} {
		assertPropertySourceSpans(t, name, stylesheetSource, explanation.Controller,
			`background-color: #010203`, `background-color`, `#010203`)
		assertPropertySourceSpans(t, name+" value source", stylesheetSource, explanation.ValueSource,
			`background-color: #010203`, `background-color`, `#010203`)
	}
	for name, explanation := range map[string]PropertyExplanation{
		"pointer inline": requirePointerExplanation(t, pointer, target, "width"),
		"stable inline":  requireStableExplanation(t, stable, targetID, "width"),
	} {
		assertPropertySourceSpans(t, name, inlineSource, explanation.Controller,
			`width: 20px !important`, `width`, `20px`)
		assertPropertySourceSpans(t, name+" value source", inlineSource, explanation.ValueSource,
			`width: 20px !important`, `width`, `20px`)
	}
}

func assertPropertySourceSpans(t *testing.T, name, source string, got PropertySource, declaration, property, value string) {
	t.Helper()
	if got.DeclarationSpan.Slice(source) != declaration || got.NameSpan.Slice(source) != property || got.ValueSpan.Slice(source) != value {
		t.Errorf("%s spans = declaration:%q name:%q value:%q, want %q/%q/%q", name,
			got.DeclarationSpan.Slice(source), got.NameSpan.Slice(source), got.ValueSpan.Slice(source),
			declaration, property, value)
	}
}

func computeProvenanceSnapshots(t *testing.T, root *dom.Node, input Input) (*Snapshot, *Snapshot, *dom.Document) {
	t.Helper()
	pointer := Compute(root, input)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	var stable *Snapshot
	err = document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		stable, computeErr = ComputeReadView(view, input)
		return computeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return pointer, stable, document
}

func provenanceNodeID(t *testing.T, document *dom.Document, node *dom.Node) dom.NodeID {
	t.Helper()
	id, ok := document.ID(node)
	if !ok {
		t.Fatalf("node %p has no stable ID", node)
	}
	return id
}

func requirePointerExplanation(t *testing.T, snapshot *Snapshot, node *dom.Node, property string) PropertyExplanation {
	t.Helper()
	explanation, ok := snapshot.Explain(node, property)
	if !ok {
		t.Fatalf("Explain(%p, %q) did not find provenance", node, property)
	}
	return explanation
}

func requireStableExplanation(t *testing.T, snapshot *Snapshot, id dom.NodeID, property string) PropertyExplanation {
	t.Helper()
	explanation, ok := snapshot.ExplainID(id, property)
	if !ok {
		t.Fatalf("ExplainID(%d, %q) did not find provenance", id, property)
	}
	return explanation
}

func assertHintRollbackExplanation(t *testing.T, explanation PropertyExplanation) {
	t.Helper()
	if explanation.Property != "width" || explanation.Value != "41px" || explanation.Resolution != ResolutionRevertLayer {
		t.Errorf("hint rollback property/value/resolution = %q/%q/%s", explanation.Property, explanation.Value, explanation.Resolution)
	}
	if explanation.Controller.Origin != CascadeOriginAuthor || explanation.Controller.Kind != SourceStylesheet || explanation.Controller.DeclarationProperty != "all" || explanation.Controller.DeclarationValue != "revert-layer" {
		t.Errorf("hint rollback controller = %#v, want author all:revert-layer", explanation.Controller)
	}
	if len(explanation.Rollback) != 0 {
		t.Errorf("hint rollback intermediate controllers = %#v, want none", explanation.Rollback)
	}
	if explanation.ValueSource.Origin != CascadeOriginPresentationalHint || explanation.ValueSource.Kind != SourcePresentationalHint || explanation.ValueSource.DeclarationProperty != "width" || explanation.ValueSource.DeclarationValue != "41" || explanation.ValueSource.Attribute != "width" {
		t.Errorf("hint rollback value source = %#v", explanation.ValueSource)
	}
}

func assertCustomRollbackExplanation(t *testing.T, explanation PropertyExplanation) {
	t.Helper()
	if explanation.Property != "--tone" || explanation.Value != "from-user" || explanation.Resolution != ResolutionRevertLayer {
		t.Errorf("custom rollback property/value/resolution = %q/%q/%s", explanation.Property, explanation.Value, explanation.Resolution)
	}
	if explanation.Controller.Origin != CascadeOriginAuthor || explanation.Controller.Kind != SourceStylesheet || explanation.Controller.DeclarationValue != "revert-layer" || explanation.Controller.Layer != "theme" {
		t.Errorf("custom rollback controller = %#v", explanation.Controller)
	}
	if len(explanation.Rollback) != 1 || explanation.Rollback[0].Origin != CascadeOriginAuthor || explanation.Rollback[0].DeclarationValue != "revert" || explanation.Rollback[0].Layer != "base" {
		t.Errorf("custom rollback chain = %#v, want one ordered base revert", explanation.Rollback)
	}
	if explanation.ValueSource.Origin != CascadeOriginUser || explanation.ValueSource.Kind != SourceStylesheet || explanation.ValueSource.DeclarationValue != "from-user" {
		t.Errorf("custom rollback value source = %#v", explanation.ValueSource)
	}
}

func assertInheritedValueSource(t *testing.T, name string, explanation PropertyExplanation) {
	t.Helper()
	if explanation.Value != "rgb(18, 52, 86)" {
		t.Errorf("%s inherited value = %q", name, explanation.Value)
	}
	if explanation.ValueSource.Origin != CascadeOriginAuthor || explanation.ValueSource.Kind != SourceInlineStyle || explanation.ValueSource.DeclarationProperty != "color" || explanation.ValueSource.DeclarationValue != "#123456" {
		t.Errorf("%s ultimate value source = %#v, want parent inline #123456", name, explanation.ValueSource)
	}
}

func assertStableOwner(t *testing.T, name string, source PropertySource, want dom.NodeID) {
	t.Helper()
	if source.OwnerID != want || source.owner != nil {
		t.Errorf("%s owner = id:%d pointer:%p, want id:%d pointer:nil", name, source.OwnerID, source.owner, want)
	}
}

func assertStableProvenanceHasNoPointers(t *testing.T, snapshot *Snapshot) {
	t.Helper()
	for index, source := range snapshot.provenance.sources {
		if source.owner != nil {
			t.Errorf("interned stable source %d retains backing owner %p", index, source.owner)
		}
	}
	if len(snapshot.provenance.byNode) != 0 {
		t.Errorf("stable provenance retains %d pointer-indexed nodes", len(snapshot.provenance.byNode))
	}
}

func assertOrdinaryDumpOrder(t *testing.T, dump string) {
	t.Helper()
	lines := strings.Split(dump, "\n")
	if len(lines) != len(propertyDefinitions) {
		t.Fatalf("dump has %d lines, want %d ordinary properties", len(lines), len(propertyDefinitions))
	}
	for index, definition := range propertyDefinitions {
		prefix := "property=" + strconv.Quote(definition.name) + " "
		if !strings.HasPrefix(lines[index], prefix) {
			t.Errorf("dump line %d = %q, want prefix %q", index, lines[index], prefix)
		}
	}
}

func assertCustomAfterOrdinaryDump(t *testing.T, dump, property string) {
	t.Helper()
	ordinary := strings.Index(dump, `property="width"`)
	custom := strings.Index(dump, "property="+strconv.Quote(property))
	if ordinary < 0 || custom < 0 || custom < ordinary {
		t.Errorf("custom property is not after ordinary registry properties:\n%s", dump)
	}
}
