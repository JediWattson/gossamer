package style

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestProvenanceEnumStrings(t *testing.T) {
	t.Parallel()

	origins := []struct {
		value CascadeOrigin
		want  string
	}{
		{CascadeOriginUnknown, "unknown"},
		{CascadeOriginUserAgent, "user-agent"},
		{CascadeOriginUser, "user"},
		{CascadeOriginPresentationalHint, "presentational-hint"},
		{CascadeOriginAuthor, "author"},
		{CascadeOrigin(255), "unknown"},
	}
	for _, test := range origins {
		if got := test.value.String(); got != test.want {
			t.Errorf("CascadeOrigin(%d).String() = %q, want %q", test.value, got, test.want)
		}
	}

	sources := []struct {
		value SourceKind
		want  string
	}{
		{SourceUnknown, "unknown"},
		{SourceInitial, "initial"},
		{SourceInherited, "inherited"},
		{SourceUserAgentRule, "user-agent-rule"},
		{SourceStylesheet, "stylesheet"},
		{SourcePresentationalHint, "presentational-hint"},
		{SourceInlineStyle, "inline-style"},
		{SourceKind(255), "unknown"},
	}
	for _, test := range sources {
		if got := test.value.String(); got != test.want {
			t.Errorf("SourceKind(%d).String() = %q, want %q", test.value, got, test.want)
		}
	}

	resolutions := []struct {
		value ResolutionKind
		want  string
	}{
		{ResolutionUnknown, "unknown"},
		{ResolutionSpecified, "specified"},
		{ResolutionInitial, "initial"},
		{ResolutionInherited, "inherited"},
		{ResolutionUnset, "unset"},
		{ResolutionInvalidAtComputedValue, "invalid-at-computed-value"},
		{ResolutionRevert, "revert"},
		{ResolutionRevertLayer, "revert-layer"},
		{ResolutionKind(255), "unknown"},
	}
	for _, test := range resolutions {
		if got := test.value.String(); got != test.want {
			t.Errorf("ResolutionKind(%d).String() = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestPropertyExplanationDumpIsDeterministicAndComplete(t *testing.T) {
	t.Parallel()

	explanation := PropertyExplanation{
		Property:   "color",
		Value:      "rgb(17, 34, 51)",
		Resolution: ResolutionRevertLayer,
		Controller: PropertySource{
			Origin:              CascadeOriginAuthor,
			Kind:                SourceInlineStyle,
			OwnerID:             9,
			DeclarationProperty: "all",
			DeclarationValue:    "revert-layer",
			Important:           true,
			SourceOrder:         18,
		},
		Rollback: []PropertySource{{
			Origin:              CascadeOriginAuthor,
			Kind:                SourceAuthorStylesheet,
			DeclarationProperty: "color",
			DeclarationValue:    "revert",
			Layer:               "theme",
			LayerRank:           4,
		}},
		ValueSource: PropertySource{
			Origin:              CascadeOriginAuthor,
			Kind:                SourceAuthorStylesheet,
			OwnerID:             2,
			DeclarationProperty: "color",
			DeclarationValue:    "#112233",
			Layer:               "base\nlayer",
			LayerRank:           3,
			Specificity:         css.Specificity{IDs: 1, Classes: 2, Types: 3},
			StylesheetOrder:     4,
			RuleOrder:           5,
			DeclarationOrder:    6,
			SourceOrder:         7,
		},
	}
	want := `property="color" value="rgb(17, 34, 51)" resolution=revert-layer controller={origin=author kind=inline-style owner=9 declaration="all" authored-value="revert-layer" declaration-span=0:0 name-span=0:0 value-span=0:0 attribute="" important=true layer="" layer-rank=0 specificity=0,0,0 stylesheet=0 rule=0 declaration-index=0 source-order=18} rollback=[{origin=author kind=stylesheet owner=0 declaration="color" authored-value="revert" declaration-span=0:0 name-span=0:0 value-span=0:0 attribute="" important=false layer="theme" layer-rank=4 specificity=0,0,0 stylesheet=0 rule=0 declaration-index=0 source-order=0}] value-source={origin=author kind=stylesheet owner=2 declaration="color" authored-value="#112233" declaration-span=0:0 name-span=0:0 value-span=0:0 attribute="" important=false layer="base\nlayer" layer-rank=3 specificity=1,2,3 stylesheet=4 rule=5 declaration-index=6 source-order=7}`
	if got := explanation.Dump(); got != want {
		t.Fatalf("Dump() =\n%s\nwant:\n%s", got, want)
	}
	if got := explanation.String(); got != want {
		t.Errorf("String() = %q, want Dump()", got)
	}
	if got := fmt.Sprint(explanation); got != want {
		t.Errorf("fmt.Sprint(explanation) = %q, want Dump()", got)
	}
}

func TestSnapshotExplanationLookupsDeriveValuesAndReturnCopies(t *testing.T) {
	t.Parallel()

	node := dom.NewElement("div")
	rollback := PropertySource{Origin: CascadeOriginAuthor, Kind: SourceStylesheet, DeclarationProperty: "color", DeclarationValue: "revert"}
	colorExplanation := PropertyExplanation{
		Property:    "color",
		Value:       "stale transient value",
		Resolution:  ResolutionRevertLayer,
		Controller:  PropertySource{Origin: CascadeOriginAuthor, Kind: SourceInlineStyle, DeclarationProperty: "all", DeclarationValue: "revert-layer"},
		ValueSource: PropertySource{Origin: CascadeOriginUser, Kind: SourceStylesheet, DeclarationProperty: "color", DeclarationValue: "#010203"},
		Rollback:    []PropertySource{rollback},
	}
	themeExplanation := PropertyExplanation{
		Property:    "--Theme",
		Value:       "also stale",
		Resolution:  ResolutionSpecified,
		Controller:  PropertySource{Origin: CascadeOriginAuthor, Kind: SourceInlineStyle, DeclarationProperty: "--Theme", DeclarationValue: "night"},
		ValueSource: PropertySource{Origin: CascadeOriginAuthor, Kind: SourceInlineStyle, DeclarationProperty: "--Theme", DeclarationValue: "night"},
	}
	transient := map[string]PropertyExplanation{"color": colorExplanation, "--Theme": themeExplanation}
	styled := &styledNode{node: node, explanations: transient}
	computed := cssInitialStyle(Viewport{InitialFontSize: 16})
	computed.color = color.NRGBA{R: 1, G: 2, B: 3, A: 0xff}
	computed.customProperties = css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{"--Theme": "night"})
	styled.style = computed
	snapshot := &Snapshot{
		byNode:     map[*dom.Node]ComputedStyle{node: computed},
		provenance: indexPointerProvenance(styled),
	}

	// Compaction must sever the transient map and rollback slice.
	transient["color"] = PropertyExplanation{Property: "color", Resolution: ResolutionInitial}
	colorExplanation.Rollback[0].Layer = "mutated"
	got, ok := snapshot.Explain(node, "COLOR")
	if !ok {
		t.Fatal("Explain(COLOR) did not find compact provenance")
	}
	if got.Value != "rgb(1, 2, 3)" {
		t.Errorf("Explain(COLOR).Value = %q, want computed snapshot value", got.Value)
	}
	if !reflect.DeepEqual(got.Rollback, []PropertySource{rollback}) {
		t.Errorf("Explain(COLOR).Rollback = %#v, want %#v", got.Rollback, []PropertySource{rollback})
	}
	got.Rollback[0].Layer = "caller mutation"
	again, ok := snapshot.Explain(node, "color")
	if !ok || !reflect.DeepEqual(again.Rollback, []PropertySource{rollback}) {
		t.Fatalf("Explain(color) after result mutation = %#v, %t", again, ok)
	}
	if got, ok := snapshot.Explain(node, "--Theme"); !ok || got.Value != "night" {
		t.Errorf("Explain(--Theme) = %#v, %t; want computed custom value", got, ok)
	}
	if _, ok := snapshot.Explain(node, "--theme"); ok {
		t.Error("custom-property lookup matched with the wrong case")
	}
	if _, ok := snapshot.Explain(nil, "color"); ok {
		t.Error("Explain(nil, color) unexpectedly succeeded")
	}
	var nilSnapshot *Snapshot
	if _, ok := nilSnapshot.Explain(node, "color"); ok {
		t.Error("nil Snapshot.Explain unexpectedly succeeded")
	}
}

func TestProvenanceStoreUsesRegistrySlotsAndInternsSources(t *testing.T) {
	t.Parallel()

	node := dom.NewElement("div")
	source := PropertySource{Origin: CascadeOriginAuthor, Kind: SourceInlineStyle, DeclarationProperty: "all", DeclarationValue: "initial"}
	store := indexPointerProvenance(&styledNode{
		node: node,
		style: computedStyle{customProperties: css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
			"--Theme": "night",
		})},
		explanations: map[string]PropertyExplanation{
			"color":   {Resolution: ResolutionInitial, Controller: source, ValueSource: source},
			"display": {Resolution: ResolutionInitial, Controller: source, ValueSource: source},
			"--Theme": {Resolution: ResolutionInitial, Controller: source, ValueSource: source},
		},
	})
	entry := store.byNode[node]
	if len(entry.ordinary) != len(propertyDefinitions) {
		t.Fatalf("len(ordinary) = %d, want registry length %d", len(entry.ordinary), len(propertyDefinitions))
	}
	if len(entry.custom) != 1 {
		t.Fatalf("len(custom) = %d, want 1", len(entry.custom))
	}
	if len(store.sources) != 1 {
		t.Fatalf("len(interned sources) = %d, want 1", len(store.sources))
	}
	colorIndex, _ := ordinaryProvenanceIndex("color")
	displayIndex, _ := ordinaryProvenanceIndex("display")
	if entry.ordinary[colorIndex].controllerID != entry.ordinary[displayIndex].controllerID {
		t.Error("identical sources were not interned to one source ID")
	}
}

func TestProvenanceInheritedCustomStorageScalesWithPropertiesPlusNodes(t *testing.T) {
	t.Parallel()

	const customCount = 512
	const descendantCount = 512
	var inline strings.Builder
	for index := 0; index < customCount; index++ {
		fmt.Fprintf(&inline, "--p%d:%d;", index, index)
	}
	documentNode := dom.NewDocument()
	sourceNode := dom.NewElement("div", dom.Attribute{Name: "style", Value: inline.String()})
	documentNode.AppendChild(sourceNode)
	deepest := sourceNode
	var immediateParent *dom.Node
	var firstChild, grandchild *dom.Node
	for index := range descendantCount {
		child := dom.NewElement("div")
		deepest.AppendChild(child)
		if index == 0 {
			firstChild = child
		}
		if index == 1 {
			grandchild = child
		}
		immediateParent = deepest
		deepest = child
	}

	input := Input{Environment: Environment{Width: 320, Height: 200, InitialFontSize: 16}}
	pointer := Compute(documentNode, input)
	assertLinearCustomProvenanceStore(t, pointer.provenance, customCount, descendantCount, false)
	firstExplanation := requirePointerExplanation(t, pointer, firstChild, "--p0")
	if firstExplanation.Controller.owner != sourceNode || firstExplanation.ValueSource.owner != sourceNode {
		t.Errorf("first inherited sources = controller %p/value %p, want source %p", firstExplanation.Controller.owner, firstExplanation.ValueSource.owner, sourceNode)
	}
	grandchildExplanation := requirePointerExplanation(t, pointer, grandchild, "--p0")
	if grandchildExplanation.Controller.owner != firstChild || grandchildExplanation.ValueSource.owner != sourceNode {
		t.Errorf("grandchild inherited sources = controller %p/value %p, want parent %p/source %p", grandchildExplanation.Controller.owner, grandchildExplanation.ValueSource.owner, firstChild, sourceNode)
	}
	if len(pointer.provenance.byNode[firstChild].custom) != 0 || len(pointer.provenance.byNode[grandchild].custom) != 0 {
		t.Error("natural inheritance retained local pointer custom records")
	}
	deepestExplanation := requirePointerExplanation(t, pointer, deepest, "--p511")
	if deepestExplanation.Value != "511" || deepestExplanation.Resolution != ResolutionInherited {
		t.Errorf("deep inherited custom value/resolution = %q/%s", deepestExplanation.Value, deepestExplanation.Resolution)
	}
	if deepestExplanation.Controller.Kind != SourceInherited || deepestExplanation.Controller.owner != immediateParent {
		t.Errorf("deep inherited controller = %#v, want immediate parent %p", deepestExplanation.Controller, immediateParent)
	}
	if deepestExplanation.ValueSource.Kind != SourceInlineStyle || deepestExplanation.ValueSource.owner != sourceNode || deepestExplanation.ValueSource.DeclarationValue != "511" {
		t.Errorf("deep inherited value source = %#v, want root inline --p511", deepestExplanation.ValueSource)
	}
	pointerDump := pointer.Dump(deepest)
	if lines := strings.Count(pointerDump, "\n") + 1; lines != len(propertyDefinitions)+customCount {
		t.Errorf("deep pointer dump lines = %d, want %d", lines, len(propertyDefinitions)+customCount)
	}
	if !strings.Contains(pointerDump, deepestExplanation.Dump()) {
		t.Error("pointer dump does not contain the synthesized inherited explanation")
	}
	assertCustomAfterOrdinaryDump(t, pointerDump, "--p0")

	document, err := dom.IndexDocument(documentNode)
	if err != nil {
		t.Fatal(err)
	}
	sourceID := provenanceNodeID(t, document, sourceNode)
	firstChildID := provenanceNodeID(t, document, firstChild)
	grandchildID := provenanceNodeID(t, document, grandchild)
	parentID := provenanceNodeID(t, document, immediateParent)
	deepestID := provenanceNodeID(t, document, deepest)
	var stable *Snapshot
	err = document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		stable, computeErr = ComputeReadView(view, input)
		return computeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLinearCustomProvenanceStore(t, stable.provenance, customCount, descendantCount, true)
	stableFirst := requireStableExplanation(t, stable, firstChildID, "--p0")
	assertStableOwner(t, "first inherited controller", stableFirst.Controller, sourceID)
	assertStableOwner(t, "first inherited value source", stableFirst.ValueSource, sourceID)
	stableGrandchild := requireStableExplanation(t, stable, grandchildID, "--p0")
	assertStableOwner(t, "grandchild inherited controller", stableGrandchild.Controller, firstChildID)
	assertStableOwner(t, "grandchild inherited value source", stableGrandchild.ValueSource, sourceID)
	if len(stable.provenance.byID[firstChildID].custom) != 0 || len(stable.provenance.byID[grandchildID].custom) != 0 {
		t.Error("natural inheritance retained local stable custom records")
	}
	stableExplanation := requireStableExplanation(t, stable, deepestID, "--p511")
	if stableExplanation.Value != "511" || stableExplanation.Resolution != ResolutionInherited {
		t.Errorf("stable deep inherited value/resolution = %q/%s", stableExplanation.Value, stableExplanation.Resolution)
	}
	assertStableOwner(t, "deep inherited controller", stableExplanation.Controller, parentID)
	assertStableOwner(t, "deep inherited value source", stableExplanation.ValueSource, sourceID)
	stableDump := stable.DumpID(deepestID)
	if lines := strings.Count(stableDump, "\n") + 1; lines != len(propertyDefinitions)+customCount {
		t.Errorf("deep stable dump lines = %d, want %d", lines, len(propertyDefinitions)+customCount)
	}
	if !strings.Contains(stableDump, stableExplanation.Dump()) {
		t.Error("stable dump does not contain the synthesized inherited explanation")
	}
	assertCustomAfterOrdinaryDump(t, stableDump, "--p0")
}

func TestProvenanceDoesNotRetainAbsentCustomRecords(t *testing.T) {
	t.Parallel()

	var inline strings.Builder
	for index := 0; index < 128; index++ {
		fmt.Fprintf(&inline, "--absent%d:initial;", index)
	}
	root := dom.NewDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: inline.String()})
	root.AppendChild(target)
	snapshot := Compute(root, Input{Environment: Environment{InitialFontSize: 16}})
	if got := len(snapshot.provenance.byNode[target].custom); got != 0 {
		t.Errorf("retained absent custom records = %d, want 0", got)
	}
	if _, ok := snapshot.Explain(target, "--absent0"); ok {
		t.Error("Explain found a custom property whose computed value is absent")
	}
}

func assertLinearCustomProvenanceStore(
	t *testing.T,
	store provenanceStore,
	customCount int,
	descendantCount int,
	stable bool,
) {
	t.Helper()
	localCustoms := 0
	parentLinks := 0
	nodeCount := len(store.byNode)
	if stable {
		nodeCount = len(store.byID)
		for id, node := range store.byID {
			localCustoms += len(node.custom)
			if node.parentID != dom.InvalidNodeID {
				parentLinks++
			}
			if node.parentNode != nil {
				t.Errorf("stable node %d retains parent pointer %p", id, node.parentNode)
			}
		}
		if len(store.byNode) != 0 {
			t.Errorf("stable store retains %d pointer-indexed nodes", len(store.byNode))
		}
	} else {
		for node, provenance := range store.byNode {
			localCustoms += len(provenance.custom)
			if provenance.parentNode != nil {
				parentLinks++
			}
			if provenance.parentID != dom.InvalidNodeID {
				t.Errorf("pointer node %p retains stable parent ID %d", node, provenance.parentID)
			}
		}
		if len(store.byID) != 0 {
			t.Errorf("pointer store retains %d stable-ID-indexed nodes", len(store.byID))
		}
	}
	if localCustoms != customCount {
		t.Errorf("retained local custom records = %d, want root-only %d", localCustoms, customCount)
	}
	if parentLinks != descendantCount {
		t.Errorf("retained parent links = %d, want one per descendant (%d)", parentLinks, descendantCount)
	}
	linearSourceBound := customCount + nodeCount*(len(propertyDefinitions)+1)
	if len(store.sources) > linearSourceBound {
		t.Errorf("interned sources = %d, exceed linear bound %d", len(store.sources), linearSourceBound)
	}
	if len(store.rollbacks) != 0 {
		t.Errorf("rollback arena has %d entries, want 0", len(store.rollbacks))
	}
}

func TestSnapshotDumpsSortPropertiesAndStableNodeIDs(t *testing.T) {
	t.Parallel()

	alpha := PropertyExplanation{Property: "background-color", Resolution: ResolutionInitial, Controller: PropertySource{Kind: SourceInitial}, ValueSource: PropertySource{Kind: SourceInitial}}
	zulu := PropertyExplanation{Property: "width", Resolution: ResolutionInitial, Controller: PropertySource{Kind: SourceInitial}, ValueSource: PropertySource{Kind: SourceInitial}}
	builder := newProvenanceBuilder(nil)
	builder.store.byID = map[dom.NodeID]nodeProvenance{
		9: builder.compact(&styledNode{explanations: map[string]PropertyExplanation{"width": zulu, "background-color": alpha}}, nil),
		2: builder.compact(&styledNode{explanations: map[string]PropertyExplanation{"width": zulu}}, nil),
	}
	computed := cssInitialStyle(Viewport{InitialFontSize: 16})
	snapshot := &Snapshot{
		byID:       map[dom.NodeID]ComputedStyle{2: computed, 9: computed},
		provenance: builder.finish(),
	}
	alpha.Value = "rgba(0, 0, 0, 0)"
	zulu.Value = "auto"

	wantNode := alpha.Dump() + "\n" + zulu.Dump()
	if got := snapshot.DumpID(9); got != wantNode {
		t.Fatalf("DumpID(9) =\n%s\nwant:\n%s", got, wantNode)
	}
	wantAll := "node=2 " + zulu.Dump() + "\n" +
		"node=9 " + alpha.Dump() + "\n" +
		"node=9 " + zulu.Dump()
	if got := snapshot.DumpExplanations(); got != wantAll {
		t.Fatalf("DumpExplanations() =\n%s\nwant:\n%s", got, wantAll)
	}
	if got, ok := snapshot.ExplainID(9, "WIDTH"); !ok || got.Value != "auto" {
		t.Errorf("ExplainID(9, WIDTH) = %#v, %t; want computed auto value", got, ok)
	}
	if _, ok := snapshot.ExplainID(dom.InvalidNodeID, "width"); ok {
		t.Error("ExplainID(InvalidNodeID, width) unexpectedly succeeded")
	}
	if got := snapshot.DumpID(dom.InvalidNodeID); got != "" {
		t.Errorf("DumpID(InvalidNodeID) = %q, want empty", got)
	}
}

func TestStablePropertyExplanationConvertsEveryPrivateOwnerToIDs(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	element := dom.NewElement("img")
	root.AppendChild(element)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}

	original := PropertyExplanation{
		Property:    "width",
		Value:       "40px",
		Resolution:  ResolutionRevertLayer,
		Controller:  PropertySource{Kind: SourcePresentationalHint, owner: element},
		ValueSource: PropertySource{Kind: SourcePresentationalHint, owner: element},
		Rollback:    []PropertySource{{Kind: SourceStylesheet, owner: element}},
	}
	err = document.WithReadView(func(view dom.ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		defer access.Close()
		wantID, ok := access.ID(element)
		if !ok {
			t.Fatal("element has no stable ID")
		}
		stable := stablePropertyExplanation(original, access)
		if stable.Controller.OwnerID != wantID || stable.ValueSource.OwnerID != wantID || stable.Rollback[0].OwnerID != wantID {
			t.Errorf("stable owner IDs = %d, %d, %d; want %d", stable.Controller.OwnerID, stable.ValueSource.OwnerID, stable.Rollback[0].OwnerID, wantID)
		}
		if stable.Controller.owner != nil || stable.ValueSource.owner != nil || stable.Rollback[0].owner != nil {
			t.Error("stable explanation retained backing node pointers")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if original.Controller.owner != element || original.ValueSource.owner != element || original.Rollback[0].owner != element {
		t.Fatal("stable conversion mutated the source explanation")
	}
}

func TestClonePropertyExplanationsDeepCopiesRollback(t *testing.T) {
	t.Parallel()

	source := map[string]PropertyExplanation{
		"color": {Rollback: []PropertySource{{Layer: "base"}}},
	}
	clone := clonePropertyExplanations(source)
	changed := clone["color"]
	changed.Rollback[0].Layer = "changed"
	clone["color"] = changed
	if got := source["color"].Rollback[0].Layer; got != "base" {
		t.Errorf("source rollback layer = %q after clone mutation, want base", got)
	}
}

func TestCompactProvenanceDoesNotDependOnMapIteration(t *testing.T) {
	t.Parallel()

	node := dom.NewElement("div")
	properties := map[string]PropertyExplanation{
		"width":   {Property: "width", Resolution: ResolutionInitial, Controller: PropertySource{Kind: SourceInitial}, ValueSource: PropertySource{Kind: SourceInitial}},
		"display": {Property: "display", Resolution: ResolutionInitial, Controller: PropertySource{Kind: SourceInitial}, ValueSource: PropertySource{Kind: SourceInitial}},
		"color":   {Property: "color", Resolution: ResolutionInitial, Controller: PropertySource{Kind: SourceInitial}, ValueSource: PropertySource{Kind: SourceInitial}},
	}
	computed := cssInitialStyle(Viewport{InitialFontSize: 16})
	snapshot := &Snapshot{
		byNode:     map[*dom.Node]ComputedStyle{node: computed},
		provenance: indexPointerProvenance(&styledNode{node: node, explanations: properties}),
	}
	got := snapshot.Dump(node)
	if !(strings.Index(got, `property="color"`) < strings.Index(got, `property="display"`) &&
		strings.Index(got, `property="display"`) < strings.Index(got, `property="width"`)) {
		t.Fatalf("dump is not in canonical property order:\n%s", got)
	}
}

func TestAbsentCustomPropertiesDoNotRetainProvenance(t *testing.T) {
	t.Parallel()

	var declarations strings.Builder
	for index := 0; index < 512; index++ {
		fmt.Fprintf(&declarations, "--absent-%d:initial;", index)
	}
	document := dom.NewDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: declarations.String()})
	document.AppendChild(target)

	snapshot := Compute(document, Input{Environment: Environment{InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain target")
	}
	if names := computed.CustomProperties().Names(); len(names) != 0 {
		t.Fatalf("computed custom properties = %d, want none", len(names))
	}
	retained := snapshot.provenance.byNode[target]
	if len(retained.custom) != 0 {
		t.Fatalf("retained absent custom provenance = %d entries, want none", len(retained.custom))
	}
	if _, ok := snapshot.Explain(target, "--absent-0"); ok {
		t.Fatal("Explain returned an absent custom property")
	}
}
