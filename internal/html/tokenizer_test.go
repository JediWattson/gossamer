package html

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestTokenizerEmitsDocumentTokens(t *testing.T) {
	t.Parallel()

	input := `<!DOCTYPE html><!--note--><DIV id="first" ID=second disabled data-x='a&amp;b'/>Hi &lt;go&#33;</DIV><?build debug?>`
	got := collectTokens(t, NewTokenizer(strings.NewReader(input)), nil)
	want := []Token{
		{Type: DoctypeToken, Data: "html"},
		{Type: CommentToken, Data: "note"},
		{
			Type: StartTagToken,
			Data: "div",
			Attributes: []Attribute{
				{Name: "id", Value: "first"},
				{Name: "disabled", Value: ""},
				{Name: "data-x", Value: "a&b"},
			},
			SelfClosing: true,
		},
		{Type: CharacterToken, Data: "Hi <go!"},
		{Type: EndTagToken, Data: "div"},
		{Type: ProcessingInstructionToken, Target: "build", Data: "debug"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens:\n%#v\nwant:\n%#v", got, want)
	}
}

func TestTokenizerNormalizesNewlines(t *testing.T) {
	t.Parallel()

	tokens := collectTokens(t, NewTokenizer(strings.NewReader("a\r\nb\rc")), nil)
	want := []Token{{Type: CharacterToken, Data: "a\nb\nc"}}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestTokenizerIgnoresLeadingByteOrderMark(t *testing.T) {
	t.Parallel()

	tokens := collectTokens(t, NewTokenizer(strings.NewReader("\ufeffhello\ufeff")), nil)
	want := []Token{{Type: CharacterToken, Data: "hello\ufeff"}}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestTokenizerRecoversCommentsAtEOF(t *testing.T) {
	t.Parallel()

	tests := map[string]Token{
		"<!--open": {Type: CommentToken, Data: "open"},
		"<!-":      {Type: CommentToken, Data: "-"},
		"<!wat":    {Type: CommentToken, Data: "wat"},
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			tokens := collectTokens(t, NewTokenizer(strings.NewReader(input)), nil)
			if !reflect.DeepEqual(tokens, []Token{want}) {
				t.Errorf("tokens = %#v, want %#v", tokens, []Token{want})
			}
		})
	}
}

func TestTokenizerAbandonsIncompleteTagAtEOF(t *testing.T) {
	t.Parallel()

	tokens := collectTokens(t, NewTokenizer(strings.NewReader("<div class=x")), nil)
	if len(tokens) != 0 {
		t.Errorf("tokens = %#v, want none", tokens)
	}
}

func TestTokenizerTextModes(t *testing.T) {
	t.Parallel()

	input := `<title>a&amp;b</title><style>a&amp;b</style><script>if (a < b) x();</not-script></script>`
	tokenizer := NewTokenizer(strings.NewReader(input))
	tokens := collectTokens(t, tokenizer, func(token Token) {
		if token.Type != StartTagToken {
			return
		}
		switch token.Data {
		case "title":
			tokenizer.enterTextMode(rcdataMode, token.Data)
		case "style":
			tokenizer.enterTextMode(rawTextMode, token.Data)
		case "script":
			tokenizer.enterTextMode(scriptDataMode, token.Data)
		}
	})
	want := []Token{
		{Type: StartTagToken, Data: "title"},
		{Type: CharacterToken, Data: "a&b"},
		{Type: EndTagToken, Data: "title"},
		{Type: StartTagToken, Data: "style"},
		{Type: CharacterToken, Data: "a&amp;b"},
		{Type: EndTagToken, Data: "style"},
		{Type: StartTagToken, Data: "script"},
		{Type: CharacterToken, Data: "if (a < b) x();</not-script>"},
		{Type: EndTagToken, Data: "script"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens:\n%#v\nwant:\n%#v", tokens, want)
	}
}

func TestTokenizerTextModeEndTagMayHaveAttributes(t *testing.T) {
	t.Parallel()

	input := `<script>x</script data-value=">">after`
	tokenizer := NewTokenizer(strings.NewReader(input))
	tokens := collectTokens(t, tokenizer, func(token Token) {
		if token.Type == StartTagToken && token.Data == "script" {
			tokenizer.enterTextMode(scriptDataMode, token.Data)
		}
	})
	want := []Token{
		{Type: StartTagToken, Data: "script"},
		{Type: CharacterToken, Data: "x"},
		{Type: EndTagToken, Data: "script"},
		{Type: CharacterToken, Data: "after"},
	}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestTokenizerIsIndependentOfReaderChunking(t *testing.T) {
	t.Parallel()

	input := "<!doctype html><main data-value='snowman ☃'><!-- x --><p>A&amp;B</p></main>"
	want := collectTokens(t, NewTokenizer(strings.NewReader(input)), nil)
	got := collectTokens(t, NewTokenizer(oneByteReader{reader: strings.NewReader(input)}), nil)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("one-byte tokens:\n%#v\nwant:\n%#v", got, want)
	}
}

func TestTokenizerBoundsCharacterTokens(t *testing.T) {
	t.Parallel()

	input := strings.Repeat("x", textTokenLimit+5)
	tokens := collectTokens(t, NewTokenizer(strings.NewReader(input)), nil)
	if len(tokens) != 2 {
		t.Fatalf("len(tokens) = %d, want 2", len(tokens))
	}
	if tokens[0].Type != CharacterToken || tokens[1].Type != CharacterToken {
		t.Fatalf("tokens = %#v, want character tokens", tokens)
	}
	if tokens[0].Data+tokens[1].Data != input {
		t.Error("character token chunks did not preserve input")
	}
}

func TestTokenizerReturnsReaderError(t *testing.T) {
	t.Parallel()

	want := errors.New("read failed")
	_, err := NewTokenizer(errorReader{err: want}).Next()
	if !errors.Is(err, want) {
		t.Errorf("Next() error = %v, want %v", err, want)
	}
}

func FuzzTokenizer(f *testing.F) {
	for _, seed := range []string{
		"",
		"plain text",
		"<p class='x'>hello &amp; goodbye</p>",
		"<!-- unfinished",
		"<?build debug?>",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		tokenizer := NewTokenizer(oneByteReader{reader: strings.NewReader(input)})
		limit := len(input)*4 + 100
		for count := 0; ; count++ {
			if count > limit {
				t.Fatal("tokenizer did not reach EOF")
			}
			_, err := tokenizer.Next()
			if err == io.EOF {
				return
			}
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
		}
	})
}

func collectTokens(t *testing.T, tokenizer *Tokenizer, afterToken func(Token)) []Token {
	t.Helper()

	var tokens []Token
	for {
		token, err := tokenizer.Next()
		if err == io.EOF {
			return tokens
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		tokens = append(tokens, token)
		if afterToken != nil {
			afterToken(token)
		}
	}
}

type oneByteReader struct {
	reader io.Reader
}

func (reader oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return reader.reader.Read(buffer)
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}
