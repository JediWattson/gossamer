package css

import "testing"

func TestStylesheetSelectorDependenciesCoverNestedDOMInputs(t *testing.T) {
	stylesheet, err := Parse(`
		article.card[data-state] > input:placeholder-shown + label { color:red }
		:is(:lang(en), #target):has(> textarea:valid) { color:blue }
	`)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := stylesheet.SelectorDependencies()
	for _, attribute := range []string{
		"class", "data-state", "placeholder", "type", "value", "lang", "xml:lang", "id",
		"disabled", "form", "max", "min", "pattern", "required",
	} {
		if !dependencies.DependsOnAttribute(attribute) {
			t.Errorf("DependsOnAttribute(%q) = false", attribute)
		}
	}
	if dependencies.DependsOnAttribute("data-unrelated") {
		t.Error("unreferenced data attribute was indexed")
	}
	if !dependencies.DependsOnCharacterData() || !dependencies.DependsOnFormState() || !dependencies.DependsOnChildList() {
		t.Fatalf("content/state/tree dependencies = %#v", dependencies)
	}
	if !dependencies.DependsOnAncestors() || !dependencies.DependsOnSiblings() || !dependencies.DependsOnDescendants() {
		t.Fatalf("directional dependencies = %#v", dependencies)
	}
}

func TestStylesheetSelectorDependenciesStayNarrowForTypeOnlyRules(t *testing.T) {
	stylesheet, err := Parse(`main, section { display:block }`)
	if err != nil {
		t.Fatal(err)
	}
	dependencies := stylesheet.SelectorDependencies()
	if dependencies.DependsOnAttribute("class") || dependencies.DependsOnCharacterData() ||
		dependencies.DependsOnFormState() || dependencies.DependsOnChildList() ||
		dependencies.DependsOnAncestors() || dependencies.DependsOnSiblings() || dependencies.DependsOnDescendants() {
		t.Fatalf("type-only dependencies = %#v, want empty", dependencies)
	}
}

func TestStylesheetSelectorDependenciesIncludeEverySelectorListBranch(t *testing.T) {
	stylesheet, err := Parse(`*, .later[data-later] { color:red }`)
	if err != nil {
		t.Fatal(err)
	}
	if dependencies := stylesheet.SelectorDependencies(); !dependencies.DependsOnAttribute("data-later") {
		t.Fatal("universal first branch suppressed later selector dependencies")
	}
}
