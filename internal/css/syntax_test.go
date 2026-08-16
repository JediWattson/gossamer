package css_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestTokenizeDecodesCoreCSSSyntaxAndRetainsSpans(t *testing.T) {
	t.Parallel()

	source := `foo\31 23 #\66 oo @media -1.5e2px 25% 3 url(a\20 b) url("x") "text" <!-- -->`
	tokens, err := css.Tokenize(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens = nonWhitespaceTokens(tokens)
	want := []struct {
		kind           css.TokenKind
		value          string
		representation string
		number         float64
		integer        bool
	}{
		{kind: css.TokenIdent, value: "foo123"},
		{kind: css.TokenHash, value: "foo"},
		{kind: css.TokenAtKeyword, value: "media"},
		{kind: css.TokenDimension, value: "px", representation: "-1.5e2", number: -150},
		{kind: css.TokenPercentage, representation: "25", number: 25, integer: true},
		{kind: css.TokenNumber, representation: "3", number: 3, integer: true},
		{kind: css.TokenURL, value: "a b"},
		{kind: css.TokenFunction, value: "url"},
		{kind: css.TokenString, value: "x"},
		{kind: css.TokenCloseParen},
		{kind: css.TokenString, value: "text"},
		{kind: css.TokenCDO},
		{kind: css.TokenCDC},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Tokenize() produced %d tokens, want %d: %#v", len(tokens), len(want), tokens)
	}
	previousEnd := 0
	for index, expected := range want {
		got := tokens[index]
		if got.Kind != expected.kind || got.Value != expected.value || got.Representation != expected.representation || got.Number != expected.number || got.Integer != expected.integer {
			t.Errorf("tokens[%d] = %#v, want kind=%s value=%q representation=%q number=%v integer=%t", index, got, expected.kind, expected.value, expected.representation, expected.number, expected.integer)
		}
		if !got.Span.Valid(len(source)) || got.Span.Start < previousEnd || got.Span.Slice(source) == "" {
			t.Errorf("tokens[%d].Span = %#v, invalid/nonmonotone for %q", index, got.Span, source)
		}
		previousEnd = got.Span.End
	}
	if !tokens[1].Identifier {
		t.Error("escaped name hash did not retain its identifier flag")
	}
	unrestricted, err := css.Tokenize(`#123`)
	if err != nil || len(unrestricted) != 1 || unrestricted[0].Kind != css.TokenHash || unrestricted[0].Identifier {
		t.Fatalf("unrestricted hash = %#v, error %v", unrestricted, err)
	}
}

func TestTokenizeCommentsPreprocessingAndStringRecovery(t *testing.T) {
	t.Parallel()

	source := "co/**/lor div/*x*/ span\r\nnext\fname\x00tail \"a\\\r\nb\" \"bad\nlast"
	tokens, err := css.Tokenize(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens = nonWhitespaceTokens(tokens)
	want := []struct {
		kind  css.TokenKind
		value string
	}{
		{css.TokenIdent, "co"},
		{css.TokenIdent, "lor"},
		{css.TokenIdent, "div"},
		{css.TokenIdent, "span"},
		{css.TokenIdent, "next"},
		{css.TokenIdent, "name�tail"},
		{css.TokenString, "ab"},
		{css.TokenBadString, "bad"},
		{css.TokenIdent, "last"},
	}
	if len(tokens) != len(want) {
		t.Fatalf("tokens = %#v, want %d non-whitespace tokens", tokens, len(want))
	}
	for index, expected := range want {
		if tokens[index].Kind != expected.kind || tokens[index].Value != expected.value {
			t.Errorf("tokens[%d] = %#v, want %s %q", index, tokens[index], expected.kind, expected.value)
		}
	}
	nul, err := css.Tokenize("\x00")
	if err != nil || len(nul) != 1 || nul[0].Kind != css.TokenIdent || nul[0].Value != "�" {
		t.Fatalf("NUL preprocessing = %#v, error %v", nul, err)
	}

	prefix, err := css.Tokenize(`color:red; content:"unterminated`)
	if err == nil || !strings.Contains(err.Error(), "unterminated string") {
		t.Fatalf("unterminated string error = %v", err)
	}
	if len(prefix) == 0 || prefix[len(prefix)-1].Kind != css.TokenString || !prefix[len(prefix)-1].Incomplete {
		t.Fatalf("unterminated string prefix = %#v", prefix)
	}
	prefix, err = css.Tokenize(`color:red; /* unterminated`)
	if err == nil || !strings.Contains(err.Error(), "unterminated comment") {
		t.Fatalf("unterminated comment error = %v", err)
	}
	prefix = nonWhitespaceTokens(prefix)
	if len(prefix) == 0 || prefix[len(prefix)-1].Kind != css.TokenSemicolon {
		t.Fatalf("unterminated comment prefix = %#v", prefix)
	}
}

func TestParseComponentValuesGroupsFunctionsAndBlocksWithImplicitEOFClosure(t *testing.T) {
	t.Parallel()

	source := `calc(1px + var(--x, [2%])) { color: red`
	values, err := css.ParseComponentValues(source)
	if err != nil {
		t.Fatal(err)
	}
	values = nonWhitespaceComponents(values)
	if len(values) != 2 || values[0].Kind != css.ComponentFunction || values[0].Token.Value != "calc" {
		t.Fatalf("top-level values = %#v", values)
	}
	if values[0].Span.Slice(source) != `calc(1px + var(--x, [2%]))` {
		t.Errorf("calc span = %q", values[0].Span.Slice(source))
	}
	var nestedFunction, nestedBlock bool
	walkComponents(values[0].Values, func(value css.ComponentValue) {
		nestedFunction = nestedFunction || value.Kind == css.ComponentFunction && value.Token.Value == "var"
		nestedBlock = nestedBlock || value.Kind == css.ComponentBlock && value.Token.Kind == css.TokenOpenSquare
	})
	if !nestedFunction || !nestedBlock {
		t.Fatalf("calc children lost nested var()/[]: %#v", values[0].Values)
	}
	if values[1].Kind != css.ComponentBlock || values[1].Token.Kind != css.TokenOpenCurly || values[1].Span.End != len(source) {
		t.Fatalf("EOF-closed block = %#v", values[1])
	}
	unclosed := `f(/**/`
	values, err = css.ParseComponentValues(unclosed)
	if err != nil || len(values) != 1 || values[0].Kind != css.ComponentFunction || values[0].Span.End != len(unclosed) {
		t.Fatalf("comment-only EOF function = %#v, error %v", values, err)
	}
}

func TestParseComponentValuesBoundsNesting(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("f(", 130) + "x" + strings.Repeat(")", 130)
	_, err := css.ParseComponentValues(source)
	if !errors.Is(err, css.ErrCSSSyntaxNestingLimit) {
		t.Fatalf("deep component error = %v, want ErrCSSSyntaxNestingLimit", err)
	}
}

func TestTokenizeBoundsInputBeforeAllocatingTokens(t *testing.T) {
	t.Parallel()

	_, err := css.Tokenize(strings.Repeat("x", (16<<20)+1))
	if !errors.Is(err, css.ErrCSSSyntaxInputLimit) {
		t.Fatalf("oversized input error = %v, want ErrCSSSyntaxInputLimit", err)
	}
}

func FuzzTokenizeAndComponentValuesDoNotPanic(f *testing.F) {
	for _, seed := range []string{
		`color: red`,
		`var(--x, calc(1px + 2%))`,
		`url(data:image/svg+xml;a,b:c)`,
		`foo\31 23/**/#\66 oo`,
		"\x00\r\n\f\"bad\nnext",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 1<<20 {
			t.Skip()
		}
		tokens, _ := css.Tokenize(source)
		previousEnd := 0
		for _, token := range tokens {
			if !token.Span.Valid(len(source)) || token.Span.Start < previousEnd {
				t.Fatalf("invalid token span %#v after %d for %d-byte input", token.Span, previousEnd, len(source))
			}
			previousEnd = token.Span.End
		}
		_, _ = css.ParseComponentValues(source)
	})
}

func nonWhitespaceTokens(tokens []css.Token) []css.Token {
	result := make([]css.Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != css.TokenWhitespace {
			result = append(result, token)
		}
	}
	return result
}

func nonWhitespaceComponents(values []css.ComponentValue) []css.ComponentValue {
	result := make([]css.ComponentValue, 0, len(values))
	for _, value := range values {
		if value.Kind == css.ComponentToken && value.Token.Kind == css.TokenWhitespace {
			continue
		}
		result = append(result, value)
	}
	return result
}

func walkComponents(values []css.ComponentValue, visit func(css.ComponentValue)) {
	for _, value := range values {
		visit(value)
		walkComponents(value.Values, visit)
	}
}
