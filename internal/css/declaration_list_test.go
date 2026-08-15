package css_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestParseRawDeclarationList(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationList(`
		COLOR: red;
		--BrandColor: ReD;
		--empty: ;
		empty: ;
		width: 1px ! ImPoRtAnT;
		--escaped-bang: \!important;
		--escaped-important: blue !\69mportant;
		content: "a;b:c";
		--invalid-bang: !;
		--invalid-close: red);
		--invalid-var: var(color);
		--invalid-fallback-semicolon: var(--missing,;);
		--invalid-fallback-bang: var(--missing,!);
		broken declaration;
		: invalid;
	`)
	if err != nil {
		t.Fatalf("ParseRawDeclarationList() error = %v", err)
	}
	want := []css.Declaration{
		{Property: "color", Value: "red"},
		{Property: "--BrandColor", Value: "ReD"},
		{Property: "--empty", Value: ""},
		{Property: "width", Value: "1px", Important: true},
		{Property: "--escaped-bang", Value: `\!important`},
		{Property: "--escaped-important", Value: "blue", Important: true},
		{Property: "content", Value: `"a;b:c"`},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseRawDeclarationList() = %#v, want %#v", declarations, want)
	}
}

func TestParseRawDeclarationListSharesCommentHandling(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationList(`
		color /* around colon */ : rgb(1, 2, 3);
		content: "/* text, not a comment */";
		--empty: /**/;
		co/* token boundary */lor: blue;
	`)
	if err != nil {
		t.Fatalf("ParseRawDeclarationList() error = %v", err)
	}
	want := []css.Declaration{
		{Property: "color", Value: "rgb(1, 2, 3)"},
		{Property: "content", Value: `"/* text, not a comment */"`},
		{Property: "--empty", Value: ""},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseRawDeclarationList() = %#v, want %#v", declarations, want)
	}
}

func TestParseRawDeclarationListPreservesEscapedStructuralCodePoints(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationList(`
		--semicolon: foo\;bar;
		--open: \(;
		--comment-marker: \/*literal*/;
		color: red;
	`)
	if err != nil {
		t.Fatalf("ParseRawDeclarationList() error = %v", err)
	}
	want := []css.Declaration{
		{Property: "--semicolon", Value: `foo\;bar`},
		{Property: "--open", Value: `\(`},
		{Property: "--comment-marker", Value: `\/*literal*/`},
		{Property: "color", Value: "red"},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseRawDeclarationList() = %#v, want %#v", declarations, want)
	}
}

func TestParseRawDeclarationListReturnsPartialDeclarationsForUnrecoverableComment(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationList(`color: red; --empty: ; /* unterminated`)
	if err == nil || !strings.Contains(err.Error(), "unterminated comment") {
		t.Fatalf("ParseRawDeclarationList() error = %v, want unterminated-comment error", err)
	}
	want := []css.Declaration{
		{Property: "color", Value: "red"},
		{Property: "--empty", Value: ""},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseRawDeclarationList() = %#v, want %#v", declarations, want)
	}
}

func TestParseRawDeclarationListReturnsPartialDeclarationsForUnrecoverableString(t *testing.T) {
	t.Parallel()

	declarations, err := css.ParseRawDeclarationList(`color: red; width: 2px; content: "unterminated`)
	if err == nil || !strings.Contains(err.Error(), "unterminated string") {
		t.Fatalf("ParseRawDeclarationList() error = %v, want unterminated-string error", err)
	}
	want := []css.Declaration{
		{Property: "color", Value: "red"},
		{Property: "width", Value: "2px"},
	}
	if !reflect.DeepEqual(declarations, want) {
		t.Fatalf("ParseRawDeclarationList() = %#v, want %#v", declarations, want)
	}
}
