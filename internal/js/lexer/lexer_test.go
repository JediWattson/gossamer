package lexer_test

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/js/lexer"
)

func TestLexTokenizesNativeSubsetWithExactSpans(t *testing.T) {
	t.Parallel()

	source := "// setup\r\nlet café = 0x28 + .5e1;\nconst text = \"go\\u{73}samer\"; café === 45;"
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.Let, lexer.Identifier, lexer.Assign, lexer.Number, lexer.Plus, lexer.Number, lexer.Semicolon,
		lexer.Const, lexer.Identifier, lexer.Assign, lexer.String, lexer.Semicolon,
		lexer.Identifier, lexer.StrictEqual, lexer.Number, lexer.Semicolon, lexer.EOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("token count = %d, want %d: %#v", len(tokens), len(want), tokens)
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
		if tokens[index].Span.End.Offset < tokens[index].Span.Start.Offset {
			t.Fatalf("token %d span = %#v", index, tokens[index].Span)
		}
		if tokens[index].Kind != lexer.EOF {
			got := source[tokens[index].Span.Start.Offset:tokens[index].Span.End.Offset]
			if got != tokens[index].Lexeme {
				t.Fatalf("token %d source = %q, lexeme %q", index, got, tokens[index].Lexeme)
			}
		}
	}
	if tokens[1].Text != "café" || tokens[1].Span.Start.Line != 2 || tokens[1].Span.Start.Column != 5 {
		t.Fatalf("Unicode identifier = %#v", tokens[1])
	}
	if tokens[3].Number != 40 || tokens[5].Number != 5 {
		t.Fatalf("numbers = %v and %v", tokens[3].Number, tokens[5].Number)
	}
	if tokens[10].Text != "gossamer" {
		t.Fatalf("decoded String = %q", tokens[10].Text)
	}
}

func TestLexRecognizesLongestOperatorsAndEscapes(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex("a!==b >>> 2 && c===\"x\\n\\x79\" ?? d=>d")
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.Identifier, lexer.StrictNotEqual, lexer.Identifier, lexer.UnsignedShiftRight, lexer.Number,
		lexer.AndAnd, lexer.Identifier, lexer.StrictEqual, lexer.String, lexer.Nullish,
		lexer.Identifier, lexer.Arrow, lexer.Identifier, lexer.EOF,
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
	if tokens[8].Text != "x\ny" {
		t.Fatalf("escaped String = %q", tokens[8].Text)
	}
}

func TestLexRecognizesOptionalChainBeforeQuestionAndDot(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex("value?.field ?? callable?.()")
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.Identifier, lexer.OptionalChain, lexer.Identifier, lexer.Nullish,
		lexer.Identifier, lexer.OptionalChain, lexer.LeftParen, lexer.RightParen, lexer.EOF,
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
}

func TestLexDoesNotTreatConditionalDecimalAsOptionalChain(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex("enabled?.82:1")
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{lexer.Identifier, lexer.Question, lexer.Number, lexer.Colon, lexer.Number, lexer.EOF}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
	if tokens[2].Lexeme != ".82" {
		t.Fatalf("decimal = %q, want .82", tokens[2].Lexeme)
	}
}

func TestLexRegexMayBeginWithEquals(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex(`value.replace(/=+$/g, ""); value /= 2;`)
	if err != nil {
		t.Fatal(err)
	}
	var sawRegex, sawAssign bool
	for _, token := range tokens {
		sawRegex = sawRegex || token.Kind == lexer.RegExp && token.Lexeme == `/=+$/g`
		sawAssign = sawAssign || token.Kind == lexer.SlashAssign
	}
	if !sawRegex || !sawAssign {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestLexRecognizesReactFrontendTokens(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex("for (let key in value) key += /[=:]/g.test(`name`); total >>>= 1;")
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.For, lexer.LeftParen, lexer.Let, lexer.Identifier, lexer.In, lexer.Identifier, lexer.RightParen,
		lexer.Identifier, lexer.PlusAssign, lexer.RegExp, lexer.Dot, lexer.Identifier, lexer.LeftParen,
		lexer.String, lexer.RightParen, lexer.Semicolon, lexer.Identifier, lexer.UnsignedShiftRightAssign,
		lexer.Number, lexer.Semicolon, lexer.EOF,
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
	if tokens[9].Text != "[=:]" || tokens[9].Flags != "g" || tokens[13].Text != "name" {
		t.Fatalf("literal tokens = %#v and %#v", tokens[9], tokens[13])
	}
}

func TestLexRecognizesStaticModuleKeywords(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex(`import value from "./value.js"; export {value as default};`)
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.Import, lexer.Identifier, lexer.Identifier, lexer.String, lexer.Semicolon,
		lexer.Export, lexer.LeftBrace, lexer.Identifier, lexer.Identifier, lexer.Default,
		lexer.RightBrace, lexer.Semicolon, lexer.EOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("token count = %d, want %d: %#v", len(tokens), len(want), tokens)
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
}

func TestLexRecognizesTemplateSubstitutionsAndNestedBraces(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex("`before ${value} between ${{nested: `inner ${other}`}.nested} after`")
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.TemplateHead, lexer.Identifier, lexer.TemplateMiddle,
		lexer.LeftBrace, lexer.Identifier, lexer.Colon, lexer.TemplateHead, lexer.Identifier, lexer.TemplateTail,
		lexer.RightBrace, lexer.Dot, lexer.Identifier, lexer.TemplateTail, lexer.EOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("token count = %d, want %d: %#v", len(tokens), len(want), tokens)
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
	if tokens[0].Text != "before " || tokens[2].Text != " between " || tokens[6].Text != "inner " || tokens[8].Text != "" || tokens[12].Text != " after" {
		t.Fatalf("template chunks = %#v", tokens)
	}
}

func TestLexRejectsMalformedInputPrecisely(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"/* never closed",
		"\"never closed",
		"1e+",
		"0x",
		"0b2",
		"1name",
		"\"\\u{110000}\"",
		"`template ${value`",
		string([]byte{0xff}),
	} {
		_, err := lexer.Lex(source)
		if !errors.Is(err, lexer.ErrInvalidToken) {
			t.Fatalf("Lex(%q) error = %v", source, err)
		}
		var problem *lexer.Error
		if !errors.As(err, &problem) || problem.Span.Start.Line == 0 || problem.Span.Start.Column == 0 {
			t.Fatalf("Lex(%q) diagnostic = %#v", source, problem)
		}
	}
}

func TestLexProductionClassAndBigIntSyntax(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex(`class Reader { #field; value = 42069n; } import "./next.js";`)
	if err != nil {
		t.Fatal(err)
	}
	var sawClass, sawPrivate, sawBigInt, sawImport bool
	for _, token := range tokens {
		switch {
		case token.Kind == lexer.Class:
			sawClass = true
		case token.Kind == lexer.PrivateIdentifier && token.Text == "field":
			sawPrivate = true
		case token.Kind == lexer.BigInt && token.Text == "42069":
			sawBigInt = true
		case token.Kind == lexer.Import:
			sawImport = true
		}
	}
	if !sawClass || !sawPrivate || !sawBigInt || !sawImport {
		t.Fatalf("tokens omitted production syntax: %#v", tokens)
	}
	if _, err := lexer.Lex(`# = 1n`); err == nil {
		t.Fatal("strict lexer accepted a malformed private identifier")
	}
}

func TestLexModernAssignmentAndExponentiationOperators(t *testing.T) {
	t.Parallel()

	tokens, err := lexer.Lex(`a ** b; a **= b; a &&= b; a ||= b; a ??= b;`)
	if err != nil {
		t.Fatal(err)
	}
	want := []lexer.Kind{
		lexer.Identifier, lexer.StarStar, lexer.Identifier, lexer.Semicolon,
		lexer.Identifier, lexer.StarStarAssign, lexer.Identifier, lexer.Semicolon,
		lexer.Identifier, lexer.AndAndAssign, lexer.Identifier, lexer.Semicolon,
		lexer.Identifier, lexer.OrOrAssign, lexer.Identifier, lexer.Semicolon,
		lexer.Identifier, lexer.NullishAssign, lexer.Identifier, lexer.Semicolon,
		lexer.EOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("token count = %d, want %d: %#v", len(tokens), len(want), tokens)
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %s, want %s", index, tokens[index].Kind, kind)
		}
	}
}

func FuzzLexNeverPanicsAndSpansStayOrdered(f *testing.F) {
	f.Add("let answer = 40 + 2;")
	f.Add("function f(x) { try { return x; } finally {} }")
	f.Fuzz(func(t *testing.T, source string) {
		tokens, err := lexer.Lex(source)
		if err != nil {
			return
		}
		var previous uint32
		for _, token := range tokens {
			if token.Span.Start.Offset < previous || token.Span.End.Offset < token.Span.Start.Offset || uint64(token.Span.End.Offset) > uint64(len(source)) {
				t.Fatalf("unordered span %#v after %d", token.Span, previous)
			}
			if token.Kind != lexer.EOF && source[token.Span.Start.Offset:token.Span.End.Offset] != token.Lexeme {
				t.Fatalf("span text differs for %#v", token)
			}
			previous = token.Span.End.Offset
		}
		if !utf8.ValidString(source) && err == nil {
			t.Fatal("invalid UTF-8 was accepted")
		}
	})
}
