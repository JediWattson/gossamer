package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
)

// lowerAsyncBody expresses suspension as ordinary Promise reactions. The
// continuation is therefore a normal native closure owned by RegionStore and
// survives the same task/Realm graph transitions as every other Function.
// Await points are lowered into nested Promise reactions so lexical bindings
// remain in the closure that owns the continuation. Straight-line statements,
// declarations, throws, and returns may surround any number of awaits.
func lowerAsyncBody(body *ast.BlockStatement) (*ast.BlockStatement, error) {
	if body == nil {
		return nil, fmt.Errorf("async Function has no body")
	}
	nextTemporary := uint32(0)
	lowered, err := lowerAsyncStatements(body.Body, body.Span(), &nextTemporary)
	if err != nil {
		return nil, err
	}
	starter := asyncPromiseResolve(body.Span(), nil)
	callback := &ast.ArrowFunctionExpression{
		Base: ast.Base{Range: body.Span()},
		Body: &ast.BlockStatement{Base: ast.Base{Range: body.Span()}, Body: lowered},
	}
	chain := asyncThen(body.Span(), starter, callback)
	return &ast.BlockStatement{
		Base: body.Base,
		Body: []ast.Statement{&ast.ReturnStatement{
			Base: ast.Base{Range: body.Span()}, Argument: chain,
		}},
	}, nil
}

func lowerAsyncStatements(statements []ast.Statement, span lexer.Span, nextTemporary *uint32) ([]ast.Statement, error) {
	if len(statements) == 0 {
		return []ast.Statement{asyncReturn(span, asyncPromiseResolve(span, nil))}, nil
	}
	statement := statements[0]
	rest := statements[1:]
	switch statement := statement.(type) {
	case *ast.ReturnStatement:
		result := statement.Argument
		if result == nil {
			result = asyncUndefined(statement.Span())
		}
		lowered, err := lowerAwaitExpression(result, func(value ast.Expression) ast.Expression {
			return asyncPromiseResolve(statement.Span(), value)
		}, nextTemporary)
		if err != nil {
			return nil, err
		}
		return []ast.Statement{asyncReturn(statement.Span(), lowered)}, nil
	case *ast.ThrowStatement:
		if containsAwait(statement.Argument) {
			lowered, err := lowerAwaitExpression(statement.Argument, func(value ast.Expression) ast.Expression {
				return asyncStatementIIFE(statement.Span(), []ast.Statement{&ast.ThrowStatement{
					Base: statement.Base, Argument: value,
				}})
			}, nextTemporary)
			if err != nil {
				return nil, err
			}
			return []ast.Statement{asyncReturn(statement.Span(), lowered)}, nil
		}
	case *ast.VariableDeclaration:
		for index, declarator := range statement.Declarations {
			if !containsAwait(declarator.Init) {
				continue
			}
			prefix := make([]ast.Statement, 0, 2)
			if index > 0 {
				prefix = append(prefix, &ast.VariableDeclaration{
					Base: statement.Base, Kind: statement.Kind, Declarations: statement.Declarations[:index],
				})
			}
			continuationInput := make([]ast.Statement, 0, len(rest)+2)
			continuationInput = append(continuationInput, &ast.VariableDeclaration{
				Base: statement.Base, Kind: statement.Kind,
				Declarations: []*ast.VariableDeclarator{{
					Base: declarator.Base, Name: declarator.Name, ArrayPattern: declarator.ArrayPattern,
					ObjectPattern: declarator.ObjectPattern, Pattern: declarator.Pattern,
				}},
			})
			if index+1 < len(statement.Declarations) {
				continuationInput = append(continuationInput, &ast.VariableDeclaration{
					Base: statement.Base, Kind: statement.Kind, Declarations: statement.Declarations[index+1:],
				})
			}
			continuationInput = append(continuationInput, rest...)
			var continuationErr error
			lowered, err := lowerAwaitExpression(declarator.Init, func(value ast.Expression) ast.Expression {
				continuationInput[0].(*ast.VariableDeclaration).Declarations[0].Init = value
				continuation, lowerErr := lowerAsyncStatements(continuationInput, span, nextTemporary)
				if lowerErr != nil {
					continuationErr = lowerErr
					return nil
				}
				return asyncStatementIIFE(statement.Span(), continuation)
			}, nextTemporary)
			if err != nil {
				return nil, err
			}
			if continuationErr != nil {
				return nil, continuationErr
			}
			if lowered == nil {
				return nil, fmt.Errorf("await continuation in variable declaration is not supported")
			}
			return append(prefix, asyncReturn(statement.Span(), lowered)), nil
		}
	case *ast.TryStatement:
		if statementContainsAwait(statement) {
			if len(rest) != 0 && statement.Finalizer != nil &&
				(statementContainsReturn(statement.Body) || statement.Handler != nil && statementContainsReturn(statement.Handler.Body)) {
				return nil, fmt.Errorf("await in try/finally with an early return followed by additional statements is not supported yet")
			}
			branchRest := rest
			if statement.Finalizer != nil {
				branchRest = nil
			}
			tryInput := append(append([]ast.Statement(nil), statement.Body.Body...), branchRest...)
			tryBody, err := lowerAsyncStatements(tryInput, statement.Body.Span(), nextTemporary)
			if err != nil {
				return nil, err
			}
			promise := asyncStatementIIFE(statement.Body.Span(), tryBody)
			if statement.Handler != nil {
				catchInput := append(append([]ast.Statement(nil), statement.Handler.Body.Body...), branchRest...)
				catchBody, err := lowerAsyncStatements(catchInput, statement.Handler.Body.Span(), nextTemporary)
				if err != nil {
					return nil, err
				}
				parameter := statement.Handler.Parameter
				if parameter == nil {
					parameter = &ast.Identifier{Base: ast.Base{Range: statement.Handler.Span()}, Name: fmt.Sprintf("\x00gossamer.catch.%d", *nextTemporary)}
					*nextTemporary++
				}
				promise = asyncPromiseMethod(statement.Span(), promise, "catch", &ast.ArrowFunctionExpression{
					Base: ast.Base{Range: statement.Handler.Span()}, Parameters: []*ast.Identifier{parameter},
					Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Handler.Body.Span()}, Body: catchBody},
				})
			}
			if statement.Finalizer != nil {
				finalizer, err := lowerAsyncStatements(statement.Finalizer.Body, statement.Finalizer.Span(), nextTemporary)
				if err != nil {
					return nil, err
				}
				promise = asyncPromiseMethod(statement.Span(), promise, "finally", &ast.ArrowFunctionExpression{
					Base: ast.Base{Range: statement.Finalizer.Span()},
					Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Finalizer.Span()}, Body: finalizer},
				})
				if len(rest) != 0 {
					continuation, err := lowerAsyncStatements(rest, span, nextTemporary)
					if err != nil {
						return nil, err
					}
					promise = asyncThen(statement.Span(), promise, &ast.ArrowFunctionExpression{
						Base: ast.Base{Range: statement.Span()},
						Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Span()}, Body: continuation},
					})
				}
			}
			return []ast.Statement{asyncReturn(statement.Span(), promise)}, nil
		}
	case *ast.IfStatement:
		if statementContainsAwait(statement) {
			var continuationErr error
			lowered, err := lowerAwaitExpression(statement.Test, func(test ast.Expression) ast.Expression {
				consequentInput := append(asyncStatementBody(statement.Consequent), rest...)
				consequent, err := lowerAsyncStatements(consequentInput, span, nextTemporary)
				if err != nil {
					continuationErr = err
					return nil
				}
				alternateInput := append(asyncStatementBody(statement.Alternate), rest...)
				alternate, err := lowerAsyncStatements(alternateInput, span, nextTemporary)
				if err != nil {
					continuationErr = err
					return nil
				}
				consequentReturn := asyncReturn(statement.Consequent.Span(), asyncStatementIIFE(statement.Consequent.Span(), consequent))
				alternateSpan := statement.Span()
				if statement.Alternate != nil {
					alternateSpan = statement.Alternate.Span()
				}
				body := []ast.Statement{
					&ast.IfStatement{Base: statement.Base, Test: test, Consequent: consequentReturn},
					asyncReturn(alternateSpan, asyncStatementIIFE(alternateSpan, alternate)),
				}
				return asyncStatementIIFE(statement.Span(), body)
			}, nextTemporary)
			if err != nil {
				return nil, err
			}
			if continuationErr != nil {
				return nil, continuationErr
			}
			return []ast.Statement{asyncReturn(statement.Span(), lowered)}, nil
		}
	case *ast.SwitchStatement:
		if statementContainsAwait(statement) {
			for _, item := range statement.Cases {
				if containsAwait(item.Test) {
					return nil, fmt.Errorf("await in switch case test is not supported yet")
				}
			}
			var switchErr error
			lowered, err := lowerAwaitExpression(statement.Discriminant, func(discriminant ast.Expression) ast.Expression {
				cases := make([]*ast.SwitchCase, len(statement.Cases))
				for index, item := range statement.Cases {
					input := make([]ast.Statement, 0)
					for cursor := index; cursor < len(statement.Cases); cursor++ {
						input = append(input, statement.Cases[cursor].Consequent...)
						if switchCaseTerminates(statement.Cases[cursor].Consequent) {
							break
						}
					}
					input = rewriteAsyncSwitchBreaks(input, rest)
					input = append(input, rest...)
					consequent, err := lowerAsyncStatements(input, span, nextTemporary)
					if err != nil {
						switchErr = err
						return nil
					}
					cases[index] = &ast.SwitchCase{Base: item.Base, Test: item.Test, Consequent: consequent}
				}
				body := []ast.Statement{
					&ast.SwitchStatement{Base: statement.Base, Discriminant: discriminant, Cases: cases},
					asyncReturn(statement.Span(), asyncStatementIIFE(statement.Span(), mustLowerAsyncStatements(rest, span, nextTemporary, &switchErr))),
				}
				return asyncStatementIIFE(statement.Span(), body)
			}, nextTemporary)
			if err != nil {
				return nil, err
			}
			if switchErr != nil {
				return nil, switchErr
			}
			return []ast.Statement{asyncReturn(statement.Span(), lowered)}, nil
		}
	case *ast.ForInStatement:
		if statementContainsAwait(statement) {
			if containsAwait(statement.Right) {
				return nil, fmt.Errorf("await in for-in/of iterable is not supported yet")
			}
			chainName := fmt.Sprintf("\x00gossamer.async.loop.%d", *nextTemporary)
			*nextTemporary++
			chain := &ast.Identifier{Base: ast.Base{Range: statement.Span()}, Name: chainName}
			stoppedName := fmt.Sprintf("\x00gossamer.async.loop.stopped.%d", *nextTemporary)
			*nextTemporary++
			stopped := &ast.Identifier{Base: ast.Base{Range: statement.Span()}, Name: stoppedName}
			declaration := &ast.VariableDeclaration{
				Base: ast.Base{Range: statement.Span()}, Kind: ast.VariableLet,
				Declarations: []*ast.VariableDeclarator{
					{Base: ast.Base{Range: statement.Span()}, Name: chain, Init: asyncPromiseResolve(statement.Span(), nil)},
					{Base: ast.Base{Range: statement.Span()}, Name: stopped, Init: &ast.BoolLiteral{Base: ast.Base{Range: statement.Span()}, Value: false}},
				},
			}
			iterationDone := asyncReturn(statement.Body.Span(), asyncPromiseResolve(statement.Body.Span(), nil))
			stop := &ast.ExpressionStatement{
				Base: ast.Base{Range: statement.Body.Span()},
				Expression: &ast.AssignmentExpression{
					Base: ast.Base{Range: statement.Body.Span()}, Operator: lexer.Assign, Left: stopped,
					Right: &ast.BoolLiteral{Base: ast.Base{Range: statement.Body.Span()}, Value: true},
				},
			}
			body := rewriteAsyncLoopControl(statement.Body, []ast.Statement{stop, iterationDone}, []ast.Statement{iterationDone})
			iterationInput := append([]ast.Statement{&ast.IfStatement{
				Base: ast.Base{Range: statement.Body.Span()}, Test: stopped, Consequent: iterationDone,
			}}, asyncStatementBody(body)...)
			iteration, err := lowerAsyncStatements(iterationInput, statement.Body.Span(), nextTemporary)
			if err != nil {
				return nil, err
			}
			callback := &ast.ArrowFunctionExpression{
				Base: ast.Base{Range: statement.Body.Span()},
				Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Body.Span()}, Body: iteration},
			}
			update := &ast.AssignmentExpression{
				Base: ast.Base{Range: statement.Body.Span()}, Operator: lexer.Assign, Left: chain,
				Right: asyncThen(statement.Body.Span(), chain, callback),
			}
			loop := &ast.ForInStatement{
				Base: statement.Base, LeftDeclaration: statement.LeftDeclaration, LeftExpression: statement.LeftExpression,
				Right: statement.Right, Of: statement.Of,
				Body: &ast.BlockStatement{
					Base: ast.Base{Range: statement.Body.Span()},
					Body: []ast.Statement{&ast.ExpressionStatement{Base: ast.Base{Range: statement.Body.Span()}, Expression: update}},
				},
			}
			continuation, err := lowerAsyncStatements(rest, span, nextTemporary)
			if err != nil {
				return nil, err
			}
			finish := asyncThen(statement.Span(), chain, &ast.ArrowFunctionExpression{
				Base: ast.Base{Range: statement.Span()},
				Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Span()}, Body: continuation},
			})
			return []ast.Statement{declaration, loop, asyncReturn(statement.Span(), finish)}, nil
		}
	case *ast.ForStatement:
		if statementContainsAwait(statement) {
			if statementContainsAwait(statement.InitDeclaration) || containsAwait(statement.InitExpression) {
				var initializer ast.Statement
				if statement.InitDeclaration != nil {
					initializer = statement.InitDeclaration
				} else {
					initializer = &ast.ExpressionStatement{Base: ast.Base{Range: statement.InitExpression.Span()}, Expression: statement.InitExpression}
				}
				loop := &ast.ForStatement{Base: statement.Base, Test: statement.Test, Update: statement.Update, Body: statement.Body}
				input := append([]ast.Statement{initializer, loop}, rest...)
				return lowerAsyncStatements(input, span, nextTemporary)
			}
			loopName := fmt.Sprintf("\x00gossamer.async.for.%d", *nextTemporary)
			*nextTemporary++
			loop := &ast.Identifier{Base: ast.Base{Range: statement.Span()}, Name: loopName}
			declaration := &ast.VariableDeclaration{
				Base: ast.Base{Range: statement.Span()}, Kind: ast.VariableLet,
				Declarations: []*ast.VariableDeclarator{{Base: ast.Base{Range: statement.Span()}, Name: loop}},
			}
			continuation, err := lowerAsyncStatements(rest, span, nextTemporary)
			if err != nil {
				return nil, err
			}
			finish := asyncReturn(statement.Span(), asyncStatementIIFE(statement.Span(), continuation))
			callLoop := func(callSpan lexer.Span) ast.Statement {
				return asyncReturn(callSpan, &ast.CallExpression{Base: ast.Base{Range: callSpan}, Callee: loop})
			}
			advance := make([]ast.Statement, 0, 2)
			if statement.Update != nil {
				advance = append(advance, &ast.ExpressionStatement{Base: ast.Base{Range: statement.Update.Span()}, Expression: statement.Update})
			}
			advance = append(advance, callLoop(statement.Span()))
			body := rewriteAsyncLoopControl(statement.Body, []ast.Statement{finish}, advance)
			iterationInput := append(asyncStatementBody(body), advance...)
			iteration, err := lowerAsyncStatements(iterationInput, statement.Body.Span(), nextTemporary)
			if err != nil {
				return nil, err
			}
			iterationResult := asyncReturn(statement.Body.Span(), asyncStatementIIFE(statement.Body.Span(), iteration))
			functionBody := make([]ast.Statement, 0, 2)
			if statement.Test != nil {
				decision := &ast.IfStatement{
					Base: ast.Base{Range: statement.Test.Span()}, Test: statement.Test,
					Consequent: iterationResult, Alternate: finish,
				}
				functionBody, err = lowerAsyncStatements([]ast.Statement{decision}, statement.Span(), nextTemporary)
				if err != nil {
					return nil, err
				}
			} else {
				functionBody = append(functionBody, iterationResult)
			}
			assignLoop := &ast.ExpressionStatement{
				Base: ast.Base{Range: statement.Span()},
				Expression: &ast.AssignmentExpression{
					Base: ast.Base{Range: statement.Span()}, Operator: lexer.Assign, Left: loop,
					Right: &ast.ArrowFunctionExpression{
						Base: ast.Base{Range: statement.Span()},
						Body: &ast.BlockStatement{Base: ast.Base{Range: statement.Span()}, Body: functionBody},
					},
				},
			}
			result := make([]ast.Statement, 0, 4)
			if statement.InitDeclaration != nil {
				result = append(result, statement.InitDeclaration)
			} else if statement.InitExpression != nil {
				result = append(result, &ast.ExpressionStatement{Base: ast.Base{Range: statement.InitExpression.Span()}, Expression: statement.InitExpression})
			}
			result = append(result, declaration, assignLoop, callLoop(statement.Span()))
			return result, nil
		}
	case *ast.WhileStatement:
		if statementContainsAwait(statement) {
			loop := &ast.ForStatement{Base: statement.Base, Test: statement.Test, Body: statement.Body}
			return lowerAsyncStatements(append([]ast.Statement{loop}, rest...), span, nextTemporary)
		}
	case *ast.DoWhileStatement:
		if statementContainsAwait(statement) {
			loopName := fmt.Sprintf("\x00gossamer.async.do.%d", *nextTemporary)
			*nextTemporary++
			loop := &ast.Identifier{Base: ast.Base{Range: statement.Span()}, Name: loopName}
			declaration := &ast.VariableDeclaration{
				Base: ast.Base{Range: statement.Span()}, Kind: ast.VariableLet,
				Declarations: []*ast.VariableDeclarator{{Base: ast.Base{Range: statement.Span()}, Name: loop}},
			}
			continuation, err := lowerAsyncStatements(rest, span, nextTemporary)
			if err != nil {
				return nil, err
			}
			finish := asyncReturn(statement.Span(), asyncStatementIIFE(statement.Span(), continuation))
			callLoop := asyncReturn(statement.Span(), &ast.CallExpression{Base: ast.Base{Range: statement.Span()}, Callee: loop})
			decision := &ast.IfStatement{
				Base: ast.Base{Range: statement.Test.Span()}, Test: statement.Test,
				Consequent: callLoop, Alternate: finish,
			}
			condition, err := lowerAsyncStatements([]ast.Statement{decision}, statement.Test.Span(), nextTemporary)
			if err != nil {
				return nil, err
			}
			body := rewriteAsyncLoopControl(statement.Body, []ast.Statement{finish}, condition)
			iteration, err := lowerAsyncStatements(append(asyncStatementBody(body), condition...), statement.Body.Span(), nextTemporary)
			if err != nil {
				return nil, err
			}
			assign := &ast.ExpressionStatement{
				Base: ast.Base{Range: statement.Span()},
				Expression: &ast.AssignmentExpression{
					Base: ast.Base{Range: statement.Span()}, Operator: lexer.Assign, Left: loop,
					Right: &ast.ArrowFunctionExpression{
						Base: ast.Base{Range: statement.Span()},
						Body: &ast.BlockStatement{
							Base: ast.Base{Range: statement.Span()},
							Body: []ast.Statement{asyncReturn(statement.Body.Span(), asyncStatementIIFE(statement.Body.Span(), iteration))},
						},
					},
				},
			}
			return []ast.Statement{declaration, assign, callLoop}, nil
		}
	case *ast.ExpressionStatement:
		if containsAwait(statement.Expression) {
			var continuationErr error
			lowered, err := lowerAwaitExpression(statement.Expression, func(value ast.Expression) ast.Expression {
				continuation, err := lowerAsyncStatements(rest, span, nextTemporary)
				if err != nil {
					continuationErr = err
					return nil
				}
				body := append([]ast.Statement{&ast.ExpressionStatement{Base: statement.Base, Expression: value}}, continuation...)
				return asyncStatementIIFE(statement.Span(), body)
			}, nextTemporary)
			if err != nil {
				return nil, err
			}
			if continuationErr != nil {
				return nil, continuationErr
			}
			return []ast.Statement{asyncReturn(statement.Span(), lowered)}, nil
		}
	}
	if statementContainsAwait(statement) {
		return nil, fmt.Errorf("await in %T control flow is not supported yet", statement)
	}
	loweredRest, err := lowerAsyncStatements(rest, span, nextTemporary)
	if err != nil {
		return nil, err
	}
	return append([]ast.Statement{statement}, loweredRest...), nil
}

func asyncStatementBody(statement ast.Statement) []ast.Statement {
	if statement == nil {
		return nil
	}
	if block, ok := statement.(*ast.BlockStatement); ok {
		return append([]ast.Statement(nil), block.Body...)
	}
	return []ast.Statement{statement}
}

func rewriteAsyncLoopControl(statement ast.Statement, breakBody, continueBody []ast.Statement) ast.Statement {
	switch statement := statement.(type) {
	case *ast.BreakStatement:
		if statement.Label == nil {
			return &ast.BlockStatement{Base: statement.Base, Body: append([]ast.Statement(nil), breakBody...)}
		}
	case *ast.ContinueStatement:
		if statement.Label == nil {
			return &ast.BlockStatement{Base: statement.Base, Body: append([]ast.Statement(nil), continueBody...)}
		}
	case *ast.BlockStatement:
		body := make([]ast.Statement, len(statement.Body))
		for index, child := range statement.Body {
			body[index] = rewriteAsyncLoopControl(child, breakBody, continueBody)
		}
		return &ast.BlockStatement{Base: statement.Base, Body: body}
	case *ast.IfStatement:
		return &ast.IfStatement{
			Base: statement.Base, Test: statement.Test,
			Consequent: rewriteAsyncLoopControl(statement.Consequent, breakBody, continueBody),
			Alternate:  rewriteAsyncLoopControl(statement.Alternate, breakBody, continueBody),
		}
	case *ast.TryStatement:
		body := rewriteAsyncLoopControl(statement.Body, breakBody, continueBody).(*ast.BlockStatement)
		var handler *ast.CatchClause
		if statement.Handler != nil {
			catchBody := rewriteAsyncLoopControl(statement.Handler.Body, breakBody, continueBody).(*ast.BlockStatement)
			handler = &ast.CatchClause{Base: statement.Handler.Base, Parameter: statement.Handler.Parameter, Body: catchBody}
		}
		var finalizer *ast.BlockStatement
		if statement.Finalizer != nil {
			finalizer = rewriteAsyncLoopControl(statement.Finalizer, breakBody, continueBody).(*ast.BlockStatement)
		}
		return &ast.TryStatement{Base: statement.Base, Body: body, Handler: handler, Finalizer: finalizer}
	case *ast.ForStatement, *ast.ForInStatement, *ast.WhileStatement, *ast.DoWhileStatement:
		// A nested loop owns its own unlabeled break and continue statements.
		return statement
	case nil:
		return nil
	}
	return statement
}

func switchCaseTerminates(statements []ast.Statement) bool {
	if len(statements) == 0 {
		return false
	}
	switch statement := statements[len(statements)-1].(type) {
	case *ast.BreakStatement, *ast.ReturnStatement, *ast.ThrowStatement:
		return true
	case *ast.BlockStatement:
		return switchCaseTerminates(statement.Body)
	}
	return false
}

func rewriteAsyncSwitchBreaks(statements []ast.Statement, continuation []ast.Statement) []ast.Statement {
	rewritten := make([]ast.Statement, len(statements))
	for index, statement := range statements {
		rewritten[index] = rewriteAsyncSwitchBreak(statement, continuation)
	}
	return rewritten
}

func rewriteAsyncSwitchBreak(statement ast.Statement, continuation []ast.Statement) ast.Statement {
	switch statement := statement.(type) {
	case *ast.BreakStatement:
		if statement.Label == nil {
			return asyncReturn(statement.Span(), asyncStatementIIFE(statement.Span(), continuation))
		}
	case *ast.BlockStatement:
		return &ast.BlockStatement{Base: statement.Base, Body: rewriteAsyncSwitchBreaks(statement.Body, continuation)}
	case *ast.IfStatement:
		return &ast.IfStatement{
			Base: statement.Base, Test: statement.Test,
			Consequent: rewriteAsyncSwitchBreak(statement.Consequent, continuation),
			Alternate:  rewriteAsyncSwitchBreak(statement.Alternate, continuation),
		}
	case *ast.ForStatement, *ast.ForInStatement, *ast.WhileStatement, *ast.DoWhileStatement, *ast.SwitchStatement:
		return statement
	case nil:
		return nil
	}
	return statement
}

func mustLowerAsyncStatements(statements []ast.Statement, span lexer.Span, nextTemporary *uint32, problem *error) []ast.Statement {
	if *problem != nil {
		return nil
	}
	lowered, err := lowerAsyncStatements(statements, span, nextTemporary)
	if err != nil {
		*problem = err
		return nil
	}
	return lowered
}

func asyncReturn(span lexer.Span, value ast.Expression) ast.Statement {
	return &ast.ReturnStatement{Base: ast.Base{Range: span}, Argument: value}
}

func asyncStatementIIFE(span lexer.Span, body []ast.Statement) ast.Expression {
	return &ast.CallExpression{
		Base: ast.Base{Range: span},
		Callee: &ast.ArrowFunctionExpression{
			Base: ast.Base{Range: span}, Body: &ast.BlockStatement{Base: ast.Base{Range: span}, Body: body},
		},
	}
}

func statementContainsAwait(statement ast.Statement) bool {
	switch statement := statement.(type) {
	case nil:
		return false
	case *ast.ExpressionStatement:
		return containsAwait(statement.Expression)
	case *ast.VariableDeclaration:
		if statement == nil {
			return false
		}
		for _, declaration := range statement.Declarations {
			if containsAwait(declaration.Init) {
				return true
			}
		}
	case *ast.ReturnStatement:
		return containsAwait(statement.Argument)
	case *ast.ThrowStatement:
		return containsAwait(statement.Argument)
	case *ast.BlockStatement:
		if statement == nil {
			return false
		}
		for _, child := range statement.Body {
			if statementContainsAwait(child) {
				return true
			}
		}
	case *ast.IfStatement:
		return containsAwait(statement.Test) || statementContainsAwait(statement.Consequent) || statementContainsAwait(statement.Alternate)
	case *ast.SwitchStatement:
		if containsAwait(statement.Discriminant) {
			return true
		}
		for _, item := range statement.Cases {
			if containsAwait(item.Test) {
				return true
			}
			for _, child := range item.Consequent {
				if statementContainsAwait(child) {
					return true
				}
			}
		}
	case *ast.WhileStatement:
		return containsAwait(statement.Test) || statementContainsAwait(statement.Body)
	case *ast.DoWhileStatement:
		return statementContainsAwait(statement.Body) || containsAwait(statement.Test)
	case *ast.ForStatement:
		return statementContainsAwait(statement.InitDeclaration) || containsAwait(statement.InitExpression) ||
			containsAwait(statement.Test) || containsAwait(statement.Update) || statementContainsAwait(statement.Body)
	case *ast.ForInStatement:
		return statementContainsAwait(statement.LeftDeclaration) || containsAwait(statement.LeftExpression) ||
			containsAwait(statement.Right) || statementContainsAwait(statement.Body)
	case *ast.TryStatement:
		if statementContainsAwait(statement.Body) || statement.Finalizer != nil && statementContainsAwait(statement.Finalizer) {
			return true
		}
		return statement.Handler != nil && statementContainsAwait(statement.Handler.Body)
	}
	return false
}

func statementContainsReturn(statement ast.Statement) bool {
	switch statement := statement.(type) {
	case nil:
		return false
	case *ast.ReturnStatement:
		return true
	case *ast.BlockStatement:
		if statement == nil {
			return false
		}
		for _, child := range statement.Body {
			if statementContainsReturn(child) {
				return true
			}
		}
	case *ast.IfStatement:
		return statementContainsReturn(statement.Consequent) || statementContainsReturn(statement.Alternate)
	case *ast.WhileStatement:
		return statementContainsReturn(statement.Body)
	case *ast.DoWhileStatement:
		return statementContainsReturn(statement.Body)
	case *ast.ForStatement:
		return statementContainsReturn(statement.Body)
	case *ast.ForInStatement:
		return statementContainsReturn(statement.Body)
	case *ast.TryStatement:
		if statementContainsReturn(statement.Body) || statement.Finalizer != nil && statementContainsReturn(statement.Finalizer) {
			return true
		}
		return statement.Handler != nil && statementContainsReturn(statement.Handler.Body)
	}
	return false
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
	case *ast.ArrayLiteral:
		elements := make([]ast.Expression, len(expression.Elements))
		var elementErr error
		var lowerElement func(int) ast.Expression
		lowerElement = func(index int) ast.Expression {
			if index == len(expression.Elements) {
				return continuation(&ast.ArrayLiteral{Base: expression.Base, Elements: elements})
			}
			if expression.Elements[index] == nil {
				return lowerElement(index + 1)
			}
			lowered, err := lowerAwaitExpression(expression.Elements[index], func(value ast.Expression) ast.Expression {
				elements[index] = value
				return lowerElement(index + 1)
			}, nextTemporary)
			if err != nil {
				elementErr = err
				return nil
			}
			return lowered
		}
		result := lowerElement(0)
		if elementErr != nil {
			return nil, elementErr
		}
		return result, nil
	case *ast.SpreadElement:
		return lowerAwaitExpression(expression.Argument, func(argument ast.Expression) ast.Expression {
			return continuation(&ast.SpreadElement{Base: expression.Base, Argument: argument})
		}, nextTemporary)
	case *ast.ObjectLiteral:
		properties := make([]*ast.ObjectProperty, len(expression.Properties))
		var propertyErr error
		var lowerProperty func(int) ast.Expression
		lowerProperty = func(index int) ast.Expression {
			if index == len(expression.Properties) {
				return continuation(&ast.ObjectLiteral{Base: expression.Base, Properties: properties})
			}
			property := expression.Properties[index]
			finish := func(key ast.Expression) ast.Expression {
				lowered, err := lowerAwaitExpression(property.Value, func(value ast.Expression) ast.Expression {
					properties[index] = &ast.ObjectProperty{
						Base: property.Base, Key: property.Key, KeyExpression: key, Value: value,
						Shorthand: property.Shorthand, Computed: property.Computed, Spread: property.Spread, Accessor: property.Accessor,
					}
					return lowerProperty(index + 1)
				}, nextTemporary)
				if err != nil {
					propertyErr = err
					return nil
				}
				return lowered
			}
			if property.Computed {
				lowered, err := lowerAwaitExpression(property.KeyExpression, finish, nextTemporary)
				if err != nil {
					propertyErr = err
					return nil
				}
				return lowered
			}
			return finish(property.KeyExpression)
		}
		result := lowerProperty(0)
		if propertyErr != nil {
			return nil, propertyErr
		}
		return result, nil
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
	case *ast.NewExpression:
		if !containsAwait(expression) {
			return continuation(expression), nil
		}
		return lowerAwaitExpression(expression.Callee, func(callee ast.Expression) ast.Expression {
			arguments := make([]ast.Expression, len(expression.Arguments))
			var lowerArgument func(int) ast.Expression
			lowerArgument = func(index int) ast.Expression {
				if index == len(expression.Arguments) {
					return continuation(&ast.NewExpression{Base: expression.Base, Callee: callee, Arguments: arguments})
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
	case *ast.ConditionalExpression:
		var branchErr error
		lowered, err := lowerAwaitExpression(expression.Test, func(test ast.Expression) ast.Expression {
			consequent, err := lowerAwaitExpression(expression.Consequent, continuation, nextTemporary)
			if err != nil {
				branchErr = err
				return nil
			}
			alternate, err := lowerAwaitExpression(expression.Alternate, continuation, nextTemporary)
			if err != nil {
				branchErr = err
				return nil
			}
			return &ast.ConditionalExpression{
				Base: expression.Base, Test: test, Consequent: consequent, Alternate: alternate,
			}
		}, nextTemporary)
		if err != nil {
			return nil, err
		}
		if branchErr != nil {
			return nil, branchErr
		}
		return lowered, nil
	case *ast.AssignmentExpression:
		if containsAwait(expression.Left) {
			return nil, fmt.Errorf("await in assignment target is not supported yet")
		}
		return lowerAwaitExpression(expression.Right, func(right ast.Expression) ast.Expression {
			return continuation(&ast.AssignmentExpression{
				Base: expression.Base, Operator: expression.Operator, Left: expression.Left, Right: right,
			})
		}, nextTemporary)
	case *ast.UnaryExpression:
		return lowerAwaitExpression(expression.Argument, func(argument ast.Expression) ast.Expression {
			return continuation(&ast.UnaryExpression{Base: expression.Base, Operator: expression.Operator, Argument: argument})
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
	case *ast.UnaryExpression:
		return containsAwait(expression.Argument)
	case *ast.UpdateExpression:
		return containsAwait(expression.Argument)
	case *ast.AssignmentExpression:
		return containsAwait(expression.Left) || containsAwait(expression.Right)
	case *ast.ConditionalExpression:
		return containsAwait(expression.Test) || containsAwait(expression.Consequent) || containsAwait(expression.Alternate)
	case *ast.ArrayLiteral:
		for _, element := range expression.Elements {
			if containsAwait(element) {
				return true
			}
		}
	case *ast.SpreadElement:
		return containsAwait(expression.Argument)
	case *ast.ObjectLiteral:
		for _, property := range expression.Properties {
			if containsAwait(property.KeyExpression) || containsAwait(property.Value) {
				return true
			}
		}
	case *ast.NewExpression:
		if containsAwait(expression.Callee) {
			return true
		}
		for _, argument := range expression.Arguments {
			if containsAwait(argument) {
				return true
			}
		}
	case *ast.TemplateLiteral:
		for _, substitution := range expression.Expressions {
			if containsAwait(substitution) {
				return true
			}
		}
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
	return asyncPromiseMethod(span, promise, "then", callback)
}

func asyncPromiseMethod(span lexer.Span, promise ast.Expression, method string, callback ast.Expression) ast.Expression {
	return &ast.CallExpression{
		Base: ast.Base{Range: span},
		Callee: &ast.MemberExpression{
			Base: ast.Base{Range: span}, Object: promise,
			Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: method},
		},
		Arguments: []ast.Expression{callback},
	}
}

func asyncUndefined(span lexer.Span) ast.Expression {
	return &ast.Identifier{Base: ast.Base{Range: span}, Name: "undefined"}
}
