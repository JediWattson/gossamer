package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestDeclarationListCSSOMOperations(t *testing.T) {
	declarations := css.ParseDeclarationList("COLOR: red; width: 10px !important; color: blue; --Theme: dark")
	want := []css.Declaration{
		{Property: "color", Value: "blue"},
		{Property: "width", Value: "10px", Important: true},
		{Property: "--Theme", Value: "dark"},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseDeclarationList() = %#v, want %#v", declarations, want)
	}
	if got, want := css.SerializeDeclarationList(declarations), "color: blue; width: 10px !important; --Theme: dark;"; got != want {
		t.Fatalf("SerializeDeclarationList() = %q, want %q", got, want)
	}
	if value, important, found := css.DeclarationValue(declarations, "WIDTH"); value != "10px" || !important || !found {
		t.Fatalf("DeclarationValue(width) = %q, %t, %t", value, important, found)
	}

	updated, changed := css.SetDeclaration(declarations, "background-color", "black", false)
	if !changed {
		t.Fatal("SetDeclaration() did not report an appended property")
	}
	updated, changed = css.SetDeclaration(updated, "width", "12px", false)
	if !changed {
		t.Fatal("SetDeclaration() did not report an updated property")
	}
	if got, want := css.DeclarationPropertyNames(updated), []string{"color", "width", "--Theme", "background-color"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeclarationPropertyNames() = %#v, want %#v", got, want)
	}

	updated, previous, changed := css.RemoveDeclaration(updated, "color")
	if previous != "blue" || !changed {
		t.Fatalf("RemoveDeclaration(color) = previous %q, changed %t", previous, changed)
	}
	if _, _, found := css.DeclarationValue(updated, "color"); found {
		t.Fatal("removed declaration remains observable")
	}
}

func TestSetDeclarationEmptyValueRemovesAndInvalidNameIsIgnored(t *testing.T) {
	declarations := css.ParseDeclarationList("color: red; width: 1px")
	updated, changed := css.SetDeclaration(declarations, "color", "", false)
	if !changed || css.SerializeDeclarationList(updated) != "width: 1px;" {
		t.Fatalf("empty SetDeclaration = %#v, changed %t", updated, changed)
	}
	unchanged, changed := css.SetDeclaration(updated, "bad name", "value", false)
	if changed || !reflect.DeepEqual(unchanged, updated) {
		t.Fatalf("invalid SetDeclaration changed %#v to %#v", updated, unchanged)
	}
}
