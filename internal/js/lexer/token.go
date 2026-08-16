// Package lexer tokenizes the explicit JavaScript subset consumed by
// Gossamer's first native parser. It performs no RegionStore allocation.
package lexer

import "fmt"

type Kind uint16

const (
	EOF Kind = iota
	Identifier
	Number
	String
	RegExp

	Let
	Const
	Var
	Function
	Return
	If
	Else
	While
	Do
	For
	In
	Switch
	Case
	Default
	Break
	Continue
	New
	This
	True
	False
	Null
	Throw
	Try
	Catch
	Finally
	Typeof
	Delete
	Instanceof
	Void

	LeftParen
	RightParen
	LeftBrace
	RightBrace
	LeftBracket
	RightBracket
	Semicolon
	Comma
	Dot
	Colon
	Question

	Assign
	Plus
	Minus
	Star
	Slash
	Percent
	PlusPlus
	MinusMinus
	Bang
	StrictEqual
	StrictNotEqual
	EqualEqual
	BangEqual
	Less
	LessEqual
	Greater
	GreaterEqual
	Ampersand
	Pipe
	Caret
	ShiftLeft
	ShiftRight
	UnsignedShiftRight
	AndAnd
	OrOr
	Nullish
	Arrow
	Tilde
	PlusAssign
	MinusAssign
	StarAssign
	SlashAssign
	PercentAssign
	AmpersandAssign
	PipeAssign
	CaretAssign
	ShiftLeftAssign
	ShiftRightAssign
	UnsignedShiftRightAssign
)

var kindNames = [...]string{
	EOF:                      "end of input",
	Identifier:               "identifier",
	Number:                   "number",
	String:                   "string",
	RegExp:                   "regular expression",
	Let:                      "let",
	Const:                    "const",
	Var:                      "var",
	Function:                 "function",
	Return:                   "return",
	If:                       "if",
	Else:                     "else",
	While:                    "while",
	Do:                       "do",
	For:                      "for",
	In:                       "in",
	Switch:                   "switch",
	Case:                     "case",
	Default:                  "default",
	Break:                    "break",
	Continue:                 "continue",
	New:                      "new",
	This:                     "this",
	True:                     "true",
	False:                    "false",
	Null:                     "null",
	Throw:                    "throw",
	Try:                      "try",
	Catch:                    "catch",
	Finally:                  "finally",
	Typeof:                   "typeof",
	Delete:                   "delete",
	Instanceof:               "instanceof",
	Void:                     "void",
	LeftParen:                "(",
	RightParen:               ")",
	LeftBrace:                "{",
	RightBrace:               "}",
	LeftBracket:              "[",
	RightBracket:             "]",
	Semicolon:                ";",
	Comma:                    ",",
	Dot:                      ".",
	Colon:                    ":",
	Question:                 "?",
	Assign:                   "=",
	Plus:                     "+",
	Minus:                    "-",
	Star:                     "*",
	Slash:                    "/",
	Percent:                  "%",
	PlusPlus:                 "++",
	MinusMinus:               "--",
	Bang:                     "!",
	StrictEqual:              "===",
	StrictNotEqual:           "!==",
	EqualEqual:               "==",
	BangEqual:                "!=",
	Less:                     "<",
	LessEqual:                "<=",
	Greater:                  ">",
	GreaterEqual:             ">=",
	Ampersand:                "&",
	Pipe:                     "|",
	Caret:                    "^",
	ShiftLeft:                "<<",
	ShiftRight:               ">>",
	UnsignedShiftRight:       ">>>",
	AndAnd:                   "&&",
	OrOr:                     "||",
	Nullish:                  "??",
	Arrow:                    "=>",
	Tilde:                    "~",
	PlusAssign:               "+=",
	MinusAssign:              "-=",
	StarAssign:               "*=",
	SlashAssign:              "/=",
	PercentAssign:            "%=",
	AmpersandAssign:          "&=",
	PipeAssign:               "|=",
	CaretAssign:              "^=",
	ShiftLeftAssign:          "<<=",
	ShiftRightAssign:         ">>=",
	UnsignedShiftRightAssign: ">>>=",
}

func (kind Kind) String() string {
	if int(kind) < len(kindNames) && kindNames[kind] != "" {
		return kindNames[kind]
	}
	return fmt.Sprintf("token(%d)", kind)
}

type Position struct {
	Offset uint32
	Line   uint32
	Column uint32
}

type Span struct {
	Start Position
	End   Position
}

// Token retains its raw source Lexeme and decoded Text or Number value. Text
// is populated for identifiers and String literals.
type Token struct {
	Kind   Kind
	Lexeme string
	Text   string
	Number float64
	Flags  string
	Span   Span
}

var keywords = map[string]Kind{
	"let":        Let,
	"const":      Const,
	"var":        Var,
	"function":   Function,
	"return":     Return,
	"if":         If,
	"else":       Else,
	"while":      While,
	"do":         Do,
	"for":        For,
	"in":         In,
	"switch":     Switch,
	"case":       Case,
	"default":    Default,
	"break":      Break,
	"continue":   Continue,
	"new":        New,
	"this":       This,
	"true":       True,
	"false":      False,
	"null":       Null,
	"throw":      Throw,
	"try":        Try,
	"catch":      Catch,
	"finally":    Finally,
	"typeof":     Typeof,
	"delete":     Delete,
	"instanceof": Instanceof,
	"void":       Void,
}
