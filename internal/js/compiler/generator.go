package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
)

// lowerGeneratorBody builds a lazy ordinary iterator from the first generator
// shape needed by production output: one for-of loop whose body ends in a
// direct or conditional yield. All state is captured by normal RegionStore
// closures, and unsupported control flow is rejected during compilation.
func lowerGeneratorBody(body *ast.BlockStatement) (*ast.BlockStatement, error) {
	if body == nil || len(body.Body) != 1 {
		return nil, fmt.Errorf("generator Function currently requires one for-of loop")
	}
	loop, ok := body.Body[0].(*ast.ForInStatement)
	if !ok || !loop.Of || loop.LeftDeclaration == nil || len(loop.LeftDeclaration.Declarations) != 1 ||
		loop.LeftDeclaration.Declarations[0].Name == nil {
		return nil, fmt.Errorf("generator Function currently requires one for-of loop with an identifier binding")
	}
	yieldBody, err := lowerGeneratorYieldStatement(loop.Body)
	if err != nil {
		return nil, err
	}
	span := body.Span()
	prefix := fmt.Sprintf("\x00gossamer.generator.%d.", span.Start.Offset)
	sourceName := prefix + "source"
	closedName := prefix + "closed"
	iteratorName := prefix + "iterator"
	stepName := prefix + "step"
	returnValueName := prefix + "return.value"

	source := generatorIdentifier(span, sourceName)
	closed := generatorIdentifier(span, closedName)
	iterator := generatorIdentifier(span, iteratorName)
	step := generatorIdentifier(span, stepName)

	initializeSource := &ast.IfStatement{
		Base: ast.Base{Range: span},
		Test: &ast.BinaryExpression{
			Base: ast.Base{Range: span}, Operator: lexer.StrictEqual,
			Left: source, Right: &ast.NullLiteral{Base: ast.Base{Range: span}},
		},
		Consequent: generatorExpressionStatement(span, &ast.AssignmentExpression{
			Base: ast.Base{Range: span}, Operator: lexer.Assign, Left: source,
			Right: generatorCall(span, &ast.MemberExpression{
				Base: ast.Base{Range: span}, Object: loop.Right,
				Property: &ast.MemberExpression{
					Base: ast.Base{Range: span}, Object: generatorIdentifier(span, "Symbol"),
					Property: generatorIdentifier(span, "iterator"),
				},
				Computed: true,
			}),
		}),
	}
	doneResult := func(value ast.Expression) ast.Expression {
		return generatorResult(span, value, true)
	}
	nextBody := &ast.BlockStatement{Base: ast.Base{Range: span}, Body: []ast.Statement{
		&ast.IfStatement{Base: ast.Base{Range: span}, Test: closed, Consequent: generatorReturn(span, doneResult(generatorUndefined(span)))},
		initializeSource,
		&ast.WhileStatement{Base: ast.Base{Range: span}, Test: &ast.BoolLiteral{Base: ast.Base{Range: span}, Value: true}, Body: &ast.BlockStatement{
			Base: ast.Base{Range: span}, Body: []ast.Statement{
				generatorVariable(span, ast.VariableConst, stepName, generatorCall(span, generatorMember(span, source, "next"))),
				&ast.IfStatement{Base: ast.Base{Range: span}, Test: generatorMember(span, step, "done"), Consequent: &ast.BlockStatement{
					Base: ast.Base{Range: span}, Body: []ast.Statement{
						generatorExpressionStatement(span, &ast.AssignmentExpression{Base: ast.Base{Range: span}, Operator: lexer.Assign, Left: closed, Right: &ast.BoolLiteral{Base: ast.Base{Range: span}, Value: true}}),
						generatorReturn(span, doneResult(generatorUndefined(span))),
					},
				}},
				generatorVariable(span, loop.LeftDeclaration.Kind, loop.LeftDeclaration.Declarations[0].Name.Name, generatorMember(span, step, "value")),
				yieldBody,
			},
		}},
	}}

	closeName := prefix + "close"
	close := generatorIdentifier(span, closeName)
	returnBody := &ast.BlockStatement{Base: ast.Base{Range: span}, Body: []ast.Statement{
		generatorExpressionStatement(span, &ast.AssignmentExpression{Base: ast.Base{Range: span}, Operator: lexer.Assign, Left: closed, Right: &ast.BoolLiteral{Base: ast.Base{Range: span}, Value: true}}),
		&ast.IfStatement{Base: ast.Base{Range: span}, Test: &ast.BinaryExpression{Base: ast.Base{Range: span}, Operator: lexer.StrictNotEqual, Left: source, Right: &ast.NullLiteral{Base: ast.Base{Range: span}}}, Consequent: &ast.BlockStatement{
			Base: ast.Base{Range: span}, Body: []ast.Statement{
				generatorVariable(span, ast.VariableConst, closeName, generatorMember(span, source, "return")),
				&ast.IfStatement{Base: ast.Base{Range: span}, Test: &ast.BinaryExpression{
					Base: ast.Base{Range: span}, Operator: lexer.StrictEqual,
					Left:  &ast.UnaryExpression{Base: ast.Base{Range: span}, Operator: lexer.Typeof, Argument: close},
					Right: &ast.StringLiteral{Base: ast.Base{Range: span}, Value: "function"},
				}, Consequent: generatorExpressionStatement(span, generatorCall(span, &ast.MemberExpression{Base: ast.Base{Range: span}, Object: close, Property: generatorIdentifier(span, "call")}, source))},
			},
		}},
		generatorReturn(span, doneResult(generatorIdentifier(span, returnValueName))),
	}}

	iteratorObject := &ast.ObjectLiteral{Base: ast.Base{Range: span}, Properties: []*ast.ObjectProperty{
		{Base: ast.Base{Range: span}, Key: "next", Value: &ast.FunctionExpression{Base: ast.Base{Range: span}, Body: nextBody}},
		{Base: ast.Base{Range: span}, Key: "return", Value: &ast.FunctionExpression{
			Base: ast.Base{Range: span}, Parameters: []*ast.Identifier{generatorIdentifier(span, returnValueName)}, Body: returnBody,
		}},
	}}
	selfIterator := &ast.FunctionExpression{Base: ast.Base{Range: span}, Body: &ast.BlockStatement{
		Base: ast.Base{Range: span}, Body: []ast.Statement{generatorReturn(span, &ast.ThisExpression{Base: ast.Base{Range: span}})},
	}}
	iteratorSymbol := &ast.MemberExpression{
		Base: ast.Base{Range: span}, Object: generatorIdentifier(span, "Symbol"), Property: generatorIdentifier(span, "iterator"),
	}
	return &ast.BlockStatement{Base: body.Base, Body: []ast.Statement{
		generatorVariable(span, ast.VariableLet, sourceName, &ast.NullLiteral{Base: ast.Base{Range: span}}),
		generatorVariable(span, ast.VariableLet, closedName, &ast.BoolLiteral{Base: ast.Base{Range: span}, Value: false}),
		generatorVariable(span, ast.VariableConst, iteratorName, iteratorObject),
		generatorExpressionStatement(span, &ast.AssignmentExpression{
			Base: ast.Base{Range: span}, Operator: lexer.Assign,
			Left:  &ast.MemberExpression{Base: ast.Base{Range: span}, Object: iterator, Property: iteratorSymbol, Computed: true},
			Right: selfIterator,
		}),
		generatorReturn(span, iterator),
	}}, nil
}

func lowerGeneratorYieldStatement(statement ast.Statement) (ast.Statement, error) {
	span := statement.Span()
	switch statement := statement.(type) {
	case *ast.ExpressionStatement:
		if yielded, ok := statement.Expression.(*ast.YieldExpression); ok {
			return generatorReturn(span, generatorResult(span, generatorYieldValue(yielded), false)), nil
		}
		if conditional, ok := statement.Expression.(*ast.BinaryExpression); ok && conditional.Operator == lexer.AndAnd {
			if yielded, ok := conditional.Right.(*ast.YieldExpression); ok {
				return &ast.IfStatement{
					Base: ast.Base{Range: span}, Test: conditional.Left,
					Consequent: generatorReturn(span, generatorResult(span, generatorYieldValue(yielded), false)),
				}, nil
			}
		}
	case *ast.IfStatement:
		consequent, err := lowerGeneratorYieldStatement(statement.Consequent)
		if err != nil {
			return nil, err
		}
		var alternate ast.Statement
		if statement.Alternate != nil {
			alternate, err = lowerGeneratorYieldStatement(statement.Alternate)
			if err != nil {
				return nil, err
			}
		}
		return &ast.IfStatement{Base: statement.Base, Test: statement.Test, Consequent: consequent, Alternate: alternate}, nil
	case *ast.BlockStatement:
		if len(statement.Body) == 1 {
			return lowerGeneratorYieldStatement(statement.Body[0])
		}
	}
	return nil, fmt.Errorf("generator loop body must end in a direct or conditional yield")
}

func generatorYieldValue(expression *ast.YieldExpression) ast.Expression {
	if expression.Argument == nil {
		return generatorUndefined(expression.Span())
	}
	return expression.Argument
}

func generatorIdentifier(span lexer.Span, name string) *ast.Identifier {
	return &ast.Identifier{Base: ast.Base{Range: span}, Name: name}
}

func generatorUndefined(span lexer.Span) ast.Expression {
	return generatorIdentifier(span, "undefined")
}

func generatorMember(span lexer.Span, object ast.Expression, name string) ast.Expression {
	return &ast.MemberExpression{Base: ast.Base{Range: span}, Object: object, Property: generatorIdentifier(span, name)}
}

func generatorCall(span lexer.Span, callee ast.Expression, arguments ...ast.Expression) ast.Expression {
	return &ast.CallExpression{Base: ast.Base{Range: span}, Callee: callee, Arguments: arguments}
}

func generatorResult(span lexer.Span, value ast.Expression, done bool) ast.Expression {
	return &ast.ObjectLiteral{Base: ast.Base{Range: span}, Properties: []*ast.ObjectProperty{
		{Base: ast.Base{Range: span}, Key: "value", Value: value},
		{Base: ast.Base{Range: span}, Key: "done", Value: &ast.BoolLiteral{Base: ast.Base{Range: span}, Value: done}},
	}}
}

func generatorReturn(span lexer.Span, value ast.Expression) ast.Statement {
	return &ast.ReturnStatement{Base: ast.Base{Range: span}, Argument: value}
}

func generatorExpressionStatement(span lexer.Span, expression ast.Expression) ast.Statement {
	return &ast.ExpressionStatement{Base: ast.Base{Range: span}, Expression: expression}
}

func generatorVariable(span lexer.Span, kind ast.VariableKind, name string, value ast.Expression) ast.Statement {
	identifier := generatorIdentifier(span, name)
	return &ast.VariableDeclaration{Base: ast.Base{Range: span}, Kind: kind, Declarations: []*ast.VariableDeclarator{
		{Base: ast.Base{Range: span}, Name: identifier, Init: value},
	}}
}
