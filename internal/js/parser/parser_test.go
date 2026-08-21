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

func TestParseBuildsTemplateLiteralExpressions(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const text = `count ${value + 1}, nested ${{answer: 42}.answer}`;")
	if err != nil {
		t.Fatal(err)
	}
	declaration := script.Body[0].(*ast.VariableDeclaration)
	template, ok := declaration.Declarations[0].Init.(*ast.TemplateLiteral)
	if !ok {
		t.Fatalf("initializer = %#v", declaration.Declarations[0].Init)
	}
	if len(template.Quasis) != 3 || len(template.Expressions) != 2 || template.Quasis[0] != "count " || template.Quasis[1] != ", nested " || template.Quasis[2] != "" {
		t.Fatalf("TemplateLiteral = %#v", template)
	}
	if _, ok := template.Expressions[0].(*ast.BinaryExpression); !ok {
		t.Fatalf("first substitution = %#v", template.Expressions[0])
	}
	if _, ok := template.Expressions[1].(*ast.MemberExpression); !ok {
		t.Fatalf("second substitution = %#v", template.Expressions[1])
	}
}

func TestParseObjectConciseMethods(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const registry = { next(value) { return value + 1; }, empty() {} };")
	if err != nil {
		t.Fatal(err)
	}
	declaration := script.Body[0].(*ast.VariableDeclaration)
	object := declaration.Declarations[0].Init.(*ast.ObjectLiteral)
	if len(object.Properties) != 2 {
		t.Fatalf("properties = %#v", object.Properties)
	}
	method, ok := object.Properties[0].Value.(*ast.FunctionExpression)
	if !ok || len(method.Parameters) != 1 || method.Parameters[0].Name != "value" {
		t.Fatalf("method = %#v", object.Properties[0].Value)
	}
	if object.Properties[0].Shorthand || object.Properties[0].Key != "next" {
		t.Fatalf("method property = %#v", object.Properties[0])
	}
}

func TestParseArrayBindingPatterns(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const [first,,third] = values; let [left, right] = pair;")
	if err != nil {
		t.Fatal(err)
	}
	first := script.Body[0].(*ast.VariableDeclaration).Declarations[0]
	if first.Name != nil || len(first.ArrayPattern) != 3 || first.ArrayPattern[0].Name != "first" || first.ArrayPattern[1] != nil || first.ArrayPattern[2].Name != "third" {
		t.Fatalf("first pattern = %#v", first)
	}
	second := script.Body[1].(*ast.VariableDeclaration).Declarations[0]
	if len(second.BindingIdentifiers()) != 2 || second.BindingIdentifiers()[1].Name != "right" {
		t.Fatalf("second pattern = %#v", second)
	}
}

func TestParseLowersDefaultParametersInSourceOrder(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("function build(first, second = first + 1, third = null) { return third; }")
	if err != nil {
		t.Fatal(err)
	}
	function := script.Body[0].(*ast.FunctionDeclaration)
	if len(function.Parameters) != 3 || len(function.Body.Body) != 3 {
		t.Fatalf("Function = %#v", function)
	}
	firstDefault, ok := function.Body.Body[0].(*ast.IfStatement)
	if !ok {
		t.Fatalf("first default prologue = %#v", function.Body.Body[0])
	}
	assignment := firstDefault.Consequent.(*ast.ExpressionStatement).Expression.(*ast.AssignmentExpression)
	if assignment.Left.(*ast.Identifier).Name != "second" {
		t.Fatalf("first default assignment = %#v", assignment)
	}
	secondDefault := function.Body.Body[1].(*ast.IfStatement)
	assignment = secondDefault.Consequent.(*ast.ExpressionStatement).Expression.(*ast.AssignmentExpression)
	if assignment.Left.(*ast.Identifier).Name != "third" {
		t.Fatalf("second default assignment = %#v", assignment)
	}
}

func TestParseLowersDestructuredArrowParameter(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const match = ([type, event]) => event === target;")
	if err != nil {
		t.Fatal(err)
	}
	arrow := script.Body[0].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.ArrowFunctionExpression)
	if len(arrow.Parameters) != 1 || arrow.Body == nil || arrow.Expression != nil || len(arrow.Body.Body) != 2 {
		t.Fatalf("ArrowFunctionExpression = %#v", arrow)
	}
	declaration := arrow.Body.Body[0].(*ast.VariableDeclaration).Declarations[0]
	if len(declaration.ArrayPattern) != 2 || declaration.ArrayPattern[0].Name != "type" || declaration.ArrayPattern[1].Name != "event" {
		t.Fatalf("arrow binding pattern = %#v", declaration)
	}
}

func TestParseRetainsSingleArraySpread(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse("const snapshot = [...node.childNodes];")
	if err != nil {
		t.Fatal(err)
	}
	array := script.Body[0].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.ArrayLiteral)
	spread, ok := array.Elements[0].(*ast.SpreadElement)
	if !ok {
		t.Fatalf("spread = %#v", array.Elements)
	}
	if _, ok := spread.Argument.(*ast.MemberExpression); !ok {
		t.Fatalf("spread argument = %#v", spread.Argument)
	}
}

func TestParseRetainsSpreadArgumentsAndObjectAccessors(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse(`const value = target.call(1, ...items); const copy = [0, ...left, ...right]; const object = {get value() { return 1; }, set value(next) { this.next = next; }};`)
	if err != nil {
		t.Fatal(err)
	}
	call := script.Body[0].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.CallExpression)
	if len(call.Arguments) != 2 {
		t.Fatalf("call arguments = %#v", call.Arguments)
	}
	if _, ok := call.Arguments[1].(*ast.SpreadElement); !ok {
		t.Fatalf("spread call argument = %#v", call.Arguments[1])
	}
	array := script.Body[1].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.ArrayLiteral)
	if len(array.Elements) != 3 {
		t.Fatalf("multi-spread array = %#v", array.Elements)
	}
	object := script.Body[2].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.ObjectLiteral)
	if object.Properties[0].Accessor != ast.ObjectPropertyGetter || object.Properties[1].Accessor != ast.ObjectPropertySetter {
		t.Fatalf("object accessors = %#v", object.Properties)
	}
}

func TestParseStaticModuleDeclarations(t *testing.T) {
	t.Parallel()

	source := `
import "./setup.js";
import primary, {value, other as renamed, default as fallback} from "./dep.js";
import * as namespace from "./namespace.js";
export const answer = value;
export function read() { return renamed; }
export {answer, read as reader};
export {remote as forwarded} from "./remote.js";
export * from "./star.js";
export * as tools from "./tools.js";
export default function named() { return fallback; }
`
	script, err := parser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Body) != 10 {
		t.Fatalf("module item count = %d", len(script.Body))
	}
	setup := script.Body[0].(*ast.ImportDeclaration)
	if setup.Source != "./setup.js" || len(setup.Specifiers) != 0 {
		t.Fatalf("side-effect import = %#v", setup)
	}
	dependency := script.Body[1].(*ast.ImportDeclaration)
	if dependency.Source != "./dep.js" || len(dependency.Specifiers) != 4 ||
		dependency.Specifiers[0].Kind != ast.ImportDefault || dependency.Specifiers[0].Local.Name != "primary" ||
		dependency.Specifiers[2].Imported != "other" || dependency.Specifiers[2].Local.Name != "renamed" ||
		dependency.Specifiers[3].Imported != "default" || dependency.Specifiers[3].Local.Name != "fallback" {
		t.Fatalf("mixed import = %#v", dependency)
	}
	namespace := script.Body[2].(*ast.ImportDeclaration)
	if namespace.Specifiers[0].Kind != ast.ImportNamespace || namespace.Specifiers[0].Local.Name != "namespace" {
		t.Fatalf("namespace import = %#v", namespace)
	}
	declaration := script.Body[3].(*ast.ExportNamedDeclaration)
	if _, ok := declaration.Declaration.(*ast.VariableDeclaration); !ok {
		t.Fatalf("exported declaration = %#v", declaration)
	}
	local := script.Body[5].(*ast.ExportNamedDeclaration)
	if local.Source != "" || len(local.Specifiers) != 2 || local.Specifiers[1].Local != "read" || local.Specifiers[1].Exported != "reader" {
		t.Fatalf("local export = %#v", local)
	}
	forwarded := script.Body[6].(*ast.ExportNamedDeclaration)
	if forwarded.Source != "./remote.js" || forwarded.Specifiers[0].Local != "remote" || forwarded.Specifiers[0].Exported != "forwarded" {
		t.Fatalf("forwarded export = %#v", forwarded)
	}
	star := script.Body[7].(*ast.ExportAllDeclaration)
	if star.Source != "./star.js" || star.Exported != "" {
		t.Fatalf("star export = %#v", star)
	}
	starNamespace := script.Body[8].(*ast.ExportAllDeclaration)
	if starNamespace.Source != "./tools.js" || starNamespace.Exported != "tools" {
		t.Fatalf("namespace export = %#v", starNamespace)
	}
	defaultExport := script.Body[9].(*ast.ExportDefaultDeclaration)
	function, ok := defaultExport.Expression.(*ast.FunctionExpression)
	if !ok || function.Name == nil || function.Name.Name != "named" {
		t.Fatalf("default export = %#v", defaultExport)
	}
}

func TestParseImportMetaAsModuleExpression(t *testing.T) {
	t.Parallel()

	script, err := parser.Parse(`
const first = import.meta;
const url = import.meta.url;
import.meta.extra = 1;
`)
	if err != nil {
		t.Fatal(err)
	}
	first := script.Body[0].(*ast.VariableDeclaration).Declarations[0].Init
	if _, ok := first.(*ast.ImportMetaExpression); !ok {
		t.Fatalf("import.meta expression = %#v", first)
	}
	url := script.Body[1].(*ast.VariableDeclaration).Declarations[0].Init.(*ast.MemberExpression)
	if _, ok := url.Object.(*ast.ImportMetaExpression); !ok || url.Computed {
		t.Fatalf("import.meta.url expression = %#v", url)
	}
	assignment := script.Body[2].(*ast.ExpressionStatement).Expression.(*ast.AssignmentExpression)
	if member, ok := assignment.Left.(*ast.MemberExpression); !ok {
		t.Fatalf("import.meta assignment = %#v", assignment.Left)
	} else if _, ok := member.Object.(*ast.ImportMetaExpression); !ok {
		t.Fatalf("import.meta assignment base = %#v", member.Object)
	}
}

func TestParseRejectsNestedAndMalformedModuleDeclarations(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`{ import value from "./dep.js"; }`,
		`if (true) export {value};`,
		`import {value as} from "./dep.js";`,
		`export * "./dep.js";`,
		`export default;`,
	} {
		if _, err := parser.Parse(source); !errors.Is(err, parser.ErrInvalidSyntax) {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
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
