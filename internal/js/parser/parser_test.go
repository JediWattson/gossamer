package parser_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/parser"
)

func TestParseBuildsCompleteSourceRangedSubsetAST(t *testing.T) {
	t.Parallel()

	source := `function add(a, b) { return a + b; }
let result = {value: add(40, 2), list: [1,,3]};
if (result.value === 42) { result.value++; } else { result.value = 0; }
while (result.value < 45) { result.value = result.value + 1; }
try { throw result.value; } catch (problem) { result.value = problem; } finally { result.value; }`
	script, err := parser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Body) != 5 || script.Span().Start.Offset != 0 || script.Span().End.Offset != uint32(len(source)) {
		t.Fatalf("Script = %#v", script)
	}
	function, ok := script.Body[0].(*ast.FunctionDeclaration)
	if !ok || function.Name.Name != "add" || len(function.Parameters) != 2 {
		t.Fatalf("Function declaration = %#v", script.Body[0])
	}
	returned, ok := function.Body.Body[0].(*ast.ReturnStatement)
	if !ok {
		t.Fatalf("Function body = %#v", function.Body.Body)
	}
	addition, ok := returned.Argument.(*ast.BinaryExpression)
	if !ok || addition.Operator != lexer.Plus {
		t.Fatalf("return expression = %#v", returned.Argument)
	}

	declaration, ok := script.Body[1].(*ast.VariableDeclaration)
	if !ok || declaration.Kind != ast.VariableLet || len(declaration.Declarations) != 1 {
		t.Fatalf("declaration = %#v", script.Body[1])
	}
	object, ok := declaration.Declarations[0].Init.(*ast.ObjectLiteral)
	if !ok || len(object.Properties) != 2 || object.Properties[0].Key != "value" {
		t.Fatalf("Object literal = %#v", declaration.Declarations[0].Init)
	}
	array, ok := object.Properties[1].Value.(*ast.ArrayLiteral)
	if !ok || len(array.Elements) != 3 || array.Elements[1] != nil {
		t.Fatalf("Array literal = %#v", object.Properties[1].Value)
	}

	branch, ok := script.Body[2].(*ast.IfStatement)
	if !ok || branch.Alternate == nil {
		t.Fatalf("if statement = %#v", script.Body[2])
	}
	comparison, ok := branch.Test.(*ast.BinaryExpression)
	if !ok || comparison.Operator != lexer.StrictEqual {
		t.Fatalf("if test = %#v", branch.Test)
	}

	loop, ok := script.Body[3].(*ast.WhileStatement)
	if !ok {
		t.Fatalf("while statement = %#v", script.Body[3])
	}
	if _, ok := loop.Test.(*ast.BinaryExpression); !ok {
		t.Fatalf("while test = %#v", loop.Test)
	}

	tryStatement, ok := script.Body[4].(*ast.TryStatement)
	if !ok || tryStatement.Handler == nil || tryStatement.Handler.Parameter.Name != "problem" || tryStatement.Finalizer == nil {
		t.Fatalf("try statement = %#v", script.Body[4])
	}
}

func TestParseHonorsPrecedenceAssociativityAndASI(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("let answer = 1 + 2 * 3 < 8 === true ? 40 : 2; answer = answer + 1;\nreturn\nanswer;")
	if err != nil {
		t.Fatal(err)
	}
	declaration := script.Body[0].(*ast.VariableDeclaration)
	conditional, ok := declaration.Declarations[0].Init.(*ast.ConditionalExpression)
	if !ok {
		t.Fatalf("initializer = %#v", declaration.Declarations[0].Init)
	}
	equality := conditional.Test.(*ast.BinaryExpression)
	if equality.Operator != lexer.StrictEqual {
		t.Fatalf("conditional test operator = %s", equality.Operator)
	}
	relational := equality.Left.(*ast.BinaryExpression)
	if relational.Operator != lexer.Less {
		t.Fatalf("relational operator = %s", relational.Operator)
	}
	addition := relational.Left.(*ast.BinaryExpression)
	if _, ok := addition.Right.(*ast.BinaryExpression); !ok || addition.Operator != lexer.Plus {
		t.Fatalf("addition tree = %#v", addition)
	}
	assignment := script.Body[1].(*ast.ExpressionStatement).Expression.(*ast.AssignmentExpression)
	if _, ok := assignment.Right.(*ast.BinaryExpression); !ok {
		t.Fatalf("right-associative assignment = %#v", assignment)
	}
	returned := script.Body[2].(*ast.ReturnStatement)
	if returned.Argument != nil {
		t.Fatalf("line-terminated return argument = %#v", returned.Argument)
	}
	if _, ok := script.Body[3].(*ast.ExpressionStatement); !ok {
		t.Fatalf("post-return statement = %#v", script.Body[3])
	}
}

func TestParseFunctionExpressionsCallsConstructionAndUpdates(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const make = function named(x) { return function() { return x; }; }; let value = new make(1); ++value.count; value.count--; make(value)();")
	if err != nil {
		t.Fatal(err)
	}
	declaration := script.Body[0].(*ast.VariableDeclaration)
	function := declaration.Declarations[0].Init.(*ast.FunctionExpression)
	if function.Name.Name != "named" || len(function.Parameters) != 1 {
		t.Fatalf("Function expression = %#v", function)
	}
	construction := script.Body[1].(*ast.VariableDeclaration).Declarations[0].Init
	if _, ok := construction.(*ast.NewExpression); !ok {
		t.Fatalf("construction = %#v", construction)
	}
	prefix := script.Body[2].(*ast.ExpressionStatement).Expression.(*ast.UpdateExpression)
	postfix := script.Body[3].(*ast.ExpressionStatement).Expression.(*ast.UpdateExpression)
	if !prefix.Prefix || postfix.Prefix {
		t.Fatalf("updates = %#v and %#v", prefix, postfix)
	}
	outerCall := script.Body[4].(*ast.ExpressionStatement).Expression.(*ast.CallExpression)
	if _, ok := outerCall.Callee.(*ast.CallExpression); !ok {
		t.Fatalf("chained call = %#v", outerCall)
	}
}

func TestParseRejectsUnsupportedOrMalformedSyntax(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"const missing;",
		"try {}",
		"1 = 2;",
		"if (true { 1; }",
		"throw\n42;",
		"break 1;",
		"({1});",
	} {
		_, err := parser.Parse(source)
		if !errors.Is(err, parser.ErrInvalidSyntax) {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
		var problem *parser.Error
		if !errors.As(err, &problem) || problem.Span.Start.Line == 0 || problem.Span.Start.Column == 0 {
			t.Fatalf("Parse(%q) diagnostic = %#v", source, problem)
		}
	}
}

func FuzzParseNeverPanicsAndScriptSpanContainsNodes(f *testing.F) {
	f.Add("let answer = 40 + 2;")
	f.Add("function f(x) { try { return x; } catch (e) { throw e; } finally {} }")
	f.Fuzz(func(t *testing.T, source string) {
		script, err := parser.Parse(source)
		if err != nil {
			return
		}
		for _, statement := range script.Body {
			span := statement.Span()
			if span.Start.Offset < script.Span().Start.Offset || span.End.Offset > script.Span().End.Offset || span.End.Offset < span.Start.Offset {
				t.Fatalf("statement span %#v outside Script %#v", span, script.Span())
			}
		}
	})
}
