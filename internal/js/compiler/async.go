package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
)

// lowerAsyncBody expresses suspension as ordinary Promise reactions. The
// continuation is therefore a normal native closure owned by RegionStore and
// survives the same task/Realm graph transitions as every other Function.
// This first rung accepts an empty body or one return statement; it covers the
// compact form emitted by Vite while keeping unsupported control flow loud.
func lowerAsyncBody(body *ast.BlockStatement) (*ast.BlockStatement, error) {
	if body == nil {
		return nil, fmt.Errorf("async Function has no body")
	}
	var result ast.Expression = asyncUndefined(body.Span())
	if len(body.Body) != 0 {
		if len(body.Body) != 1 {
			return nil, fmt.Errorf("async Function currently requires a single return statement")
		}
		returned, ok := body.Body[0].(*ast.ReturnStatement)
		if !ok {
			return nil, fmt.Errorf("async Function currently requires a single return statement")
		}
		if returned.Argument != nil {
			result = returned.Argument
		}
	}

	nextTemporary := uint32(0)
	lowered, err := lowerAwaitExpression(result, func(value ast.Expression) ast.Expression { return value }, &nextTemporary)
	if err != nil {
		return nil, err
	}
	starter := asyncPromiseResolve(body.Span(), nil)
	callback := &ast.ArrowFunctionExpression{
		Base:       ast.Base{Range: body.Span()},
		Expression: lowered,
	}
	chain := asyncThen(body.Span(), starter, callback)
	return &ast.BlockStatement{
		Base: body.Base,
		Body: []ast.Statement{&ast.ReturnStatement{
			Base: ast.Base{Range: body.Span()}, Argument: chain,
		}},
	}, nil
}

type awaitContinuation func(ast.Expression) ast.Expression

func lowerAwaitExpression(expression ast.Expression, continuation awaitContinuation, nextTemporary *uint32) (ast.Expression, error) {
	if expression == nil {
		return continuation(asyncUndefined(lexer.Span{})), nil
	}
	switch expression := expression.(type) {
	case *ast.AwaitExpression:
		return lowerAwaitExpression(expression.Argument, func(awaited ast.Expression) ast.Expression {
			name := fmt.Sprintf("\x00gossamer.await.%d", *nextTemporary)
			*nextTemporary++
			parameter := &ast.Identifier{Base: ast.Base{Range: expression.Span()}, Name: name}
			callback := &ast.ArrowFunctionExpression{
				Base:       ast.Base{Range: expression.Span()},
				Parameters: []*ast.Identifier{parameter},
				Expression: continuation(parameter),
			}
			return asyncThen(expression.Span(), asyncPromiseResolve(expression.Span(), awaited), callback)
		}, nextTemporary)
	case *ast.SequenceExpression:
		values := make([]ast.Expression, len(expression.Expressions))
		var lowerAt func(int) (ast.Expression, error)
		lowerAt = func(index int) (ast.Expression, error) {
			if index == len(expression.Expressions) {
				return continuation(&ast.SequenceExpression{Base: expression.Base, Expressions: values}), nil
			}
			return lowerAwaitExpression(expression.Expressions[index], func(value ast.Expression) ast.Expression {
				values[index] = value
				result, err := lowerAt(index + 1)
				if err != nil {
					return nil
				}
				return result
			}, nextTemporary)
		}
		return lowerAt(0)
	case *ast.MemberExpression:
		if !containsAwait(expression) {
			return continuation(expression), nil
		}
		return lowerAwaitExpression(expression.Object, func(object ast.Expression) ast.Expression {
			member := &ast.MemberExpression{Base: expression.Base, Object: object, Property: expression.Property, Computed: expression.Computed}
			return continuation(member)
		}, nextTemporary)
	case *ast.CallExpression:
		if !containsAwait(expression) {
			return continuation(expression), nil
		}
		return lowerAwaitExpression(expression.Callee, func(callee ast.Expression) ast.Expression {
			arguments := make([]ast.Expression, len(expression.Arguments))
			var lowerArgument func(int) ast.Expression
			lowerArgument = func(index int) ast.Expression {
				if index == len(expression.Arguments) {
					return continuation(&ast.CallExpression{Base: expression.Base, Callee: callee, Arguments: arguments})
				}
				lowered, err := lowerAwaitExpression(expression.Arguments[index], func(value ast.Expression) ast.Expression {
					arguments[index] = value
					return lowerArgument(index + 1)
				}, nextTemporary)
				if err != nil {
					return nil
				}
				return lowered
			}
			return lowerArgument(0)
		}, nextTemporary)
	case *ast.BinaryExpression:
		if !containsAwait(expression) {
			return continuation(expression), nil
		}
		return lowerAwaitExpression(expression.Left, func(left ast.Expression) ast.Expression {
			lowered, _ := lowerAwaitExpression(expression.Right, func(right ast.Expression) ast.Expression {
				return continuation(&ast.BinaryExpression{Base: expression.Base, Operator: expression.Operator, Left: left, Right: right})
			}, nextTemporary)
			return lowered
		}, nextTemporary)
	case *ast.DynamicImportExpression:
		if !containsAwait(expression.Source) {
			return continuation(expression), nil
		}
		return lowerAwaitExpression(expression.Source, func(source ast.Expression) ast.Expression {
			return continuation(&ast.DynamicImportExpression{Base: expression.Base, Source: source})
		}, nextTemporary)
	default:
		if containsAwait(expression) {
			return nil, fmt.Errorf("await in %T is not supported yet", expression)
		}
		return continuation(expression), nil
	}
}

func containsAwait(expression ast.Expression) bool {
	switch expression := expression.(type) {
	case nil:
		return false
	case *ast.AwaitExpression:
		return true
	case *ast.SequenceExpression:
		for _, item := range expression.Expressions {
			if containsAwait(item) {
				return true
			}
		}
	case *ast.MemberExpression:
		return containsAwait(expression.Object) || (expression.Computed && containsAwait(expression.Property))
	case *ast.CallExpression:
		if containsAwait(expression.Callee) {
			return true
		}
		for _, argument := range expression.Arguments {
			if containsAwait(argument) {
				return true
			}
		}
	case *ast.BinaryExpression:
		return containsAwait(expression.Left) || containsAwait(expression.Right)
	case *ast.DynamicImportExpression:
		return containsAwait(expression.Source)
	}
	return false
}

func asyncPromiseResolve(span lexer.Span, argument ast.Expression) ast.Expression {
	arguments := []ast.Expression(nil)
	if argument != nil {
		arguments = []ast.Expression{argument}
	}
	return &ast.CallExpression{
		Base: ast.Base{Range: span},
		Callee: &ast.MemberExpression{
			Base:     ast.Base{Range: span},
			Object:   &ast.Identifier{Base: ast.Base{Range: span}, Name: "Promise"},
			Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: "resolve"},
		},
		Arguments: arguments,
	}
}

func asyncThen(span lexer.Span, promise ast.Expression, callback ast.Expression) ast.Expression {
	return &ast.CallExpression{
		Base: ast.Base{Range: span},
		Callee: &ast.MemberExpression{
			Base: ast.Base{Range: span}, Object: promise,
			Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: "then"},
		},
		Arguments: []ast.Expression{callback},
	}
}

func asyncUndefined(span lexer.Span) ast.Expression {
	return &ast.Identifier{Base: ast.Base{Range: span}, Name: "undefined"}
}
