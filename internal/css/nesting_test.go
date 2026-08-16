package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestParseExpandsNestedSelectorsGroupsAndTrailingDeclarations(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		.card, #hero {
			color: black;
			&.active { color: red }
			> .title, .body { width: 10px }
			@media screen {
				opacity: .5;
				&:hover { opacity: 1 }
			}
			@supports (display: block) { .supported { display: block } }
			@layer components { & .layered { color: blue } }
			.after { height: 20px }
			background-color: white;
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.Rules), 9; got != want {
		t.Fatalf("rule count = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	if got, want := stylesheet.Rules[0].Declarations, []css.Declaration{{Property: "color", Value: "black"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent declarations = %#v, want %#v", got, want)
	}
	if got, want := stylesheet.Rules[8].Declarations, []css.Declaration{{Property: "background-color", Value: "white"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trailing parent declarations = %#v, want %#v", got, want)
	}

	card := dom.NewElement("section", dom.Attribute{Name: "class", Value: "card active"})
	title := dom.NewElement("h2", dom.Attribute{Name: "class", Value: "title"})
	body := dom.NewElement("div", dom.Attribute{Name: "class", Value: "body"})
	supported := dom.NewElement("div", dom.Attribute{Name: "class", Value: "supported"})
	layered := dom.NewElement("div", dom.Attribute{Name: "class", Value: "layered"})
	after := dom.NewElement("div", dom.Attribute{Name: "class", Value: "after"})
	card.AppendChild(title)
	card.AppendChild(body)
	card.AppendChild(supported)
	card.AppendChild(layered)
	card.AppendChild(after)

	if specificity, matched := stylesheet.Rules[1].Match(card); !matched || specificity != (css.Specificity{IDs: 1, Classes: 1}) {
		t.Fatalf("&.active match = %#v, %t", specificity, matched)
	}
	for _, node := range []*dom.Node{title, body} {
		if specificity, matched := stylesheet.Rules[2].Match(node); !matched {
			t.Fatalf("relative/implicit nested selector did not match %q", node.Attributes[0].Value)
		} else if specificity != (css.Specificity{IDs: 1, Classes: 1}) {
			t.Fatalf("nested selector specificity = %#v, want parent-list max plus child class", specificity)
		}
	}
	if got, want := stylesheet.Rules[3].Media, []string{"screen"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nested group declaration media = %q, want %q", got, want)
	}
	if _, matched := stylesheet.Rules[4].Match(card); matched {
		t.Fatal("dynamic &:hover unexpectedly matched without hover state")
	}
	if _, matched := stylesheet.Rules[5].Match(supported); !matched {
		t.Fatal("nested @supports selector did not match")
	}
	if got := stylesheet.Rules[5].Supports; !reflect.DeepEqual(got, []string{"(display: block)"}) {
		t.Fatalf("nested supports context = %q", got)
	}
	if _, matched := stylesheet.Rules[6].Match(layered); !matched || stylesheet.Rules[6].Layer != "components" {
		t.Fatalf("nested layer rule = matched %t, layer %q", matched, stylesheet.Rules[6].Layer)
	}
	if _, matched := stylesheet.Rules[7].Match(after); !matched {
		t.Fatal("implicit descendant after nested groups did not match")
	}
}

func TestParseSupportsMultiLevelNestingAndRejectsNonLeadingAmpersands(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		.outer {
			.child {
				& > span { color: red }
			}
			& + & { color: blue }
			:is(&) { color: green }
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("retained rules = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	outer := dom.NewElement("div", dom.Attribute{Name: "class", Value: "outer"})
	child := dom.NewElement("div", dom.Attribute{Name: "class", Value: "child"})
	span := dom.NewElement("span")
	child.AppendChild(span)
	outer.AppendChild(child)
	if _, matched := stylesheet.Rules[0].Match(span); !matched {
		t.Fatal("multi-level nested selector did not match")
	}
}

func TestNestedDeclarationRecoveryPreservesCustomCurlyValues(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		.parent {
			--tokens: { color: red; };
			.child { color: blue }
			width: 10px;
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.Rules), 3; got != want {
		t.Fatalf("rule count = %d, want %d", got, want)
	}
	if got, want := stylesheet.Rules[0].Declarations, []css.Declaration{{Property: "--tokens", Value: "{ color: red; }"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("leading parent declarations = %#v, want %#v", got, want)
	}
	if got, want := stylesheet.Rules[2].Declarations, []css.Declaration{{Property: "width", Value: "10px"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trailing parent declarations = %#v, want %#v", got, want)
	}
}
