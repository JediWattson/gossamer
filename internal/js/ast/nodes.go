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
	Name          *Identifier
	ArrayPattern  []*Identifier
	ObjectPattern []*ObjectBindingProperty
	Pattern       *BindingPattern
	Init          Expression
}

type ObjectBindingProperty struct {
	Base
	Key     string
	Binding *Identifier
	Default Expression
}

// BindingPattern represents an identifier or recursively nested array/object
// binding pattern. ArrayPattern and ObjectPattern above remain populated for
// simple patterns so existing AST consumers can continue to inspect them.
type BindingPattern struct {
	Base
	Name   *Identifier
	Array  []*BindingElement
	Object []*ObjectBindingElement
}

type BindingElement struct {
	Base
	Pattern *BindingPattern
	Default Expression
	Rest    bool
}

type ObjectBindingElement struct {
	Base
	Key     string
	Pattern *BindingPattern
	Default Expression
	Rest    bool
}

func (pattern *BindingPattern) BindingIdentifiers() []*Identifier {
	if pattern == nil {
		return nil
	}
	if pattern.Name != nil {
		return []*Identifier{pattern.Name}
	}
	bindings := make([]*Identifier, 0)
	for _, element := range pattern.Array {
		if element != nil {
			bindings = append(bindings, element.Pattern.BindingIdentifiers()...)
		}
	}
	for _, element := range pattern.Object {
		if element != nil {
			bindings = append(bindings, element.Pattern.BindingIdentifiers()...)
		}
	}
	return bindings
}

func (declarator *VariableDeclarator) BindingIdentifiers() []*Identifier {
	if declarator == nil {
		return nil
	}
	if declarator.Name != nil {
		return []*Identifier{declarator.Name}
	}
	if declarator.Pattern != nil {
		return declarator.Pattern.BindingIdentifiers()
	}
	if declarator.ObjectPattern != nil {
		bindings := make([]*Identifier, 0, len(declarator.ObjectPattern))
		for _, property := range declarator.ObjectPattern {
			if property != nil && property.Binding != nil {
				bindings = append(bindings, property.Binding)
			}
		}
		return bindings
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
	Generator  bool
}

func (*FunctionDeclaration) statementNode() {}

type ClassElementKind uint8

const (
	ClassField ClassElementKind = iota + 1
	ClassMethod
	ClassGetter
	ClassSetter
	ClassConstructor
)

// ClassElement retains the source order required by field initialization and
// static installation. Private keys are already mapped by the parser to a
// class-unique, non-source property name; they never become public strings.
type ClassElement struct {
	Base
	Kind          ClassElementKind
	Key           string
	KeyExpression Expression
	Value         Expression
	Function      *FunctionExpression
	Computed      bool
	Private       bool
	Static        bool
}

type ClassDeclaration struct {
	Base
	Name         *Identifier
	SuperClass   Expression
	SuperBinding string
	Elements     []*ClassElement
}

func (*ClassDeclaration) statementNode() {}

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

// BigIntLiteral stores the source integer without its trailing n. Radix
// prefixes are preserved so the portable program loader can parse it exactly.
type BigIntLiteral struct {
	Base
	Text string
}

func (*BigIntLiteral) expressionNode() {}

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

type YieldExpression struct {
	Base
	Argument Expression
}

func (*YieldExpression) expressionNode() {}

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
	Key           string
	KeyExpression Expression
	Value         Expression
	Shorthand     bool
	Computed      bool
	Spread        bool
	Accessor      ObjectPropertyAccessor
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
	Optional bool
}

func (*MemberExpression) expressionNode() {}

type CallExpression struct {
	Base
	Callee    Expression
	Arguments []Expression
	Optional  bool
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
	Generator  bool
}

func (*FunctionExpression) expressionNode() {}

type ClassExpression struct {
	Base
	Name         *Identifier
	SuperClass   Expression
	SuperBinding string
	Elements     []*ClassElement
}

func (*ClassExpression) expressionNode() {}

type ArrowFunctionExpression struct {
	Base
	Parameters []*Identifier
	Body       *BlockStatement
	Expression Expression
	Async      bool
}

func (*ArrowFunctionExpression) expressionNode() {}

func IsAssignmentTarget(expression Expression) bool {
	switch expression := expression.(type) {
	case *Identifier:
		return true
	case *MemberExpression:
		return !memberContainsOptionalChain(expression)
	case *ArrayLiteral:
		for _, element := range expression.Elements {
			if element == nil {
				continue
			}
			if spread, ok := element.(*SpreadElement); ok {
				element = spread.Argument
			}
			if assignment, ok := element.(*AssignmentExpression); ok && assignment.Operator == lexer.Assign {
				element = assignment.Left
			}
			if !IsAssignmentTarget(element) {
				return false
			}
		}
		return true
	case *ObjectLiteral:
		for _, property := range expression.Properties {
			if property == nil || property.Accessor != ObjectPropertyData {
				return false
			}
			target := property.Value
			if assignment, ok := target.(*AssignmentExpression); ok && assignment.Operator == lexer.Assign {
				target = assignment.Left
			}
			if !IsAssignmentTarget(target) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func memberContainsOptionalChain(member *MemberExpression) bool {
	if member.Optional {
		return true
	}
	switch object := member.Object.(type) {
	case *MemberExpression:
		return memberContainsOptionalChain(object)
	case *CallExpression:
		return object.Optional || expressionContainsOptionalChain(object.Callee)
	default:
		return false
	}
}

func expressionContainsOptionalChain(expression Expression) bool {
	switch expression := expression.(type) {
	case *MemberExpression:
		return memberContainsOptionalChain(expression)
	case *CallExpression:
		return expression.Optional || expressionContainsOptionalChain(expression.Callee)
	default:
		return false
	}
}
