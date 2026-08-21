// Package ast defines the source-ranged syntax tree for Gossamer's first
// native JavaScript subset. Nodes contain no runtime Values or Refs.
package ast

import "github.com/JediWattson/gossamer/internal/js/lexer"

type Node interface {
	Span() lexer.Span
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

type Base struct {
	Range lexer.Span
}

func (base Base) Span() lexer.Span { return base.Range }

type Script struct {
	Base
	Body []Statement
}

type ImportSpecifierKind uint8

const (
	ImportDefault ImportSpecifierKind = iota + 1
	ImportNamed
	ImportNamespace
)

type ImportSpecifier struct {
	Base
	Kind     ImportSpecifierKind
	Imported string
	Local    *Identifier
}

type ImportDeclaration struct {
	Base
	Specifiers []*ImportSpecifier
	Source     string
}

func (*ImportDeclaration) statementNode() {}

type ExportSpecifier struct {
	Base
	Local    string
	Exported string
}

// ExportNamedDeclaration represents either an exported declaration, a local
// export list, or a re-export list when Source is non-empty.
type ExportNamedDeclaration struct {
	Base
	Declaration Statement
	Specifiers  []*ExportSpecifier
	Source      string
}

func (*ExportNamedDeclaration) statementNode() {}

// ExportDefaultDeclaration contains exactly one of Declaration or Expression.
type ExportDefaultDeclaration struct {
	Base
	Declaration Statement
	Expression  Expression
}

func (*ExportDefaultDeclaration) statementNode() {}

// ExportAllDeclaration represents export * from and export * as namespace
// from. Exported is empty for the former.
type ExportAllDeclaration struct {
	Base
	Exported string
	Source   string
}

func (*ExportAllDeclaration) statementNode() {}

type EmptyStatement struct{ Base }

func (*EmptyStatement) statementNode() {}

type BlockStatement struct {
	Base
	Body []Statement
}

func (*BlockStatement) statementNode() {}

type ExpressionStatement struct {
	Base
	Expression Expression
}

func (*ExpressionStatement) statementNode() {}

type VariableKind uint8

const (
	VariableLet VariableKind = iota + 1
	VariableConst
	VariableVar
)

type VariableDeclarator struct {
	Base
	Name         *Identifier
	ArrayPattern []*Identifier
	Init         Expression
}

func (declarator *VariableDeclarator) BindingIdentifiers() []*Identifier {
	if declarator == nil {
		return nil
	}
	if declarator.Name != nil {
		return []*Identifier{declarator.Name}
	}
	bindings := make([]*Identifier, 0, len(declarator.ArrayPattern))
	for _, identifier := range declarator.ArrayPattern {
		if identifier != nil {
			bindings = append(bindings, identifier)
		}
	}
	return bindings
}

type VariableDeclaration struct {
	Base
	Kind         VariableKind
	Declarations []*VariableDeclarator
}

func (*VariableDeclaration) statementNode() {}

type FunctionDeclaration struct {
	Base
	Name       *Identifier
	Parameters []*Identifier
	Body       *BlockStatement
	Async      bool
}

func (*FunctionDeclaration) statementNode() {}

type ReturnStatement struct {
	Base
	Argument Expression
}

func (*ReturnStatement) statementNode() {}

type IfStatement struct {
	Base
	Test       Expression
	Consequent Statement
	Alternate  Statement
}

func (*IfStatement) statementNode() {}

type WhileStatement struct {
	Base
	Test Expression
	Body Statement
}

func (*WhileStatement) statementNode() {}

type DoWhileStatement struct {
	Base
	Body Statement
	Test Expression
}

func (*DoWhileStatement) statementNode() {}

type ForStatement struct {
	Base
	InitDeclaration *VariableDeclaration
	InitExpression  Expression
	Test            Expression
	Update          Expression
	Body            Statement
}

func (*ForStatement) statementNode() {}

type ForInStatement struct {
	Base
	LeftDeclaration *VariableDeclaration
	LeftExpression  Expression
	Right           Expression
	Body            Statement
	Of              bool
}

func (*ForInStatement) statementNode() {}

type SwitchCase struct {
	Base
	Test       Expression
	Consequent []Statement
}

type SwitchStatement struct {
	Base
	Discriminant Expression
	Cases        []*SwitchCase
}

func (*SwitchStatement) statementNode() {}

type LabeledStatement struct {
	Base
	Label *Identifier
	Body  Statement
}

func (*LabeledStatement) statementNode() {}

type BreakStatement struct {
	Base
	Label *Identifier
}

func (*BreakStatement) statementNode() {}

type ContinueStatement struct {
	Base
	Label *Identifier
}

func (*ContinueStatement) statementNode() {}

type ThrowStatement struct {
	Base
	Argument Expression
}

func (*ThrowStatement) statementNode() {}

type CatchClause struct {
	Base
	Parameter *Identifier
	Body      *BlockStatement
}

type TryStatement struct {
	Base
	Body      *BlockStatement
	Handler   *CatchClause
	Finalizer *BlockStatement
}

func (*TryStatement) statementNode() {}

type Identifier struct {
	Base
	Name string
}

func (*Identifier) expressionNode() {}

type NumberLiteral struct {
	Base
	Value float64
}

func (*NumberLiteral) expressionNode() {}

type StringLiteral struct {
	Base
	Value string
}

func (*StringLiteral) expressionNode() {}

type TemplateLiteral struct {
	Base
	Quasis      []string
	Expressions []Expression
}

func (*TemplateLiteral) expressionNode() {}

type BoolLiteral struct {
	Base
	Value bool
}

func (*BoolLiteral) expressionNode() {}

type NullLiteral struct{ Base }

func (*NullLiteral) expressionNode() {}

type RegExpLiteral struct {
	Base
	Pattern string
	Flags   string
}

func (*RegExpLiteral) expressionNode() {}

type ThisExpression struct{ Base }

func (*ThisExpression) expressionNode() {}

// ImportMetaExpression is the module-scoped import.meta meta-property. The
// canonical object and URL are supplied by the module host during linking.
type ImportMetaExpression struct{ Base }

func (*ImportMetaExpression) expressionNode() {}

// DynamicImportExpression is the Promise-returning import(specifier) form.
// Browser graph loading currently prefetches literal specifiers outside the
// engine; the expression remains general so runtime lookup rejects any source
// that was not present in that graph.
type DynamicImportExpression struct {
	Base
	Source Expression
}

func (*DynamicImportExpression) expressionNode() {}

type AwaitExpression struct {
	Base
	Argument Expression
}

func (*AwaitExpression) expressionNode() {}

type ArrayLiteral struct {
	Base
	// A nil element is an array hole.
	Elements []Expression
}

func (*ArrayLiteral) expressionNode() {}

type SpreadElement struct {
	Base
	Argument Expression
}

func (*SpreadElement) expressionNode() {}

type ObjectProperty struct {
	Base
	Key       string
	Value     Expression
	Shorthand bool
	Accessor  ObjectPropertyAccessor
}

type ObjectPropertyAccessor uint8

const (
	ObjectPropertyData ObjectPropertyAccessor = iota
	ObjectPropertyGetter
	ObjectPropertySetter
)

type ObjectLiteral struct {
	Base
	Properties []*ObjectProperty
}

func (*ObjectLiteral) expressionNode() {}

type UnaryExpression struct {
	Base
	Operator lexer.Kind
	Argument Expression
}

func (*UnaryExpression) expressionNode() {}

type UpdateExpression struct {
	Base
	Operator lexer.Kind
	Argument Expression
	Prefix   bool
}

func (*UpdateExpression) expressionNode() {}

type BinaryExpression struct {
	Base
	Operator lexer.Kind
	Left     Expression
	Right    Expression
}

func (*BinaryExpression) expressionNode() {}

type ConditionalExpression struct {
	Base
	Test       Expression
	Consequent Expression
	Alternate  Expression
}

func (*ConditionalExpression) expressionNode() {}

type AssignmentExpression struct {
	Base
	Operator lexer.Kind
	Left     Expression
	Right    Expression
}

func (*AssignmentExpression) expressionNode() {}

type SequenceExpression struct {
	Base
	Expressions []Expression
}

func (*SequenceExpression) expressionNode() {}

type MemberExpression struct {
	Base
	Object   Expression
	Property Expression
	Computed bool
}

func (*MemberExpression) expressionNode() {}

type CallExpression struct {
	Base
	Callee    Expression
	Arguments []Expression
}

func (*CallExpression) expressionNode() {}

type NewExpression struct {
	Base
	Callee    Expression
	Arguments []Expression
}

func (*NewExpression) expressionNode() {}

type FunctionExpression struct {
	Base
	Name       *Identifier
	Parameters []*Identifier
	Body       *BlockStatement
	Async      bool
}

func (*FunctionExpression) expressionNode() {}

type ArrowFunctionExpression struct {
	Base
	Parameters []*Identifier
	Body       *BlockStatement
	Expression Expression
	Async      bool
}

func (*ArrowFunctionExpression) expressionNode() {}

func IsAssignmentTarget(expression Expression) bool {
	switch expression.(type) {
	case *Identifier, *MemberExpression:
		return true
	default:
		return false
	}
}
