// Package parser builds a source-ranged AST for Gossamer's explicit native
// JavaScript subset.
package parser

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
)

var ErrInvalidSyntax = errors.New("js/parser: invalid syntax")

type Error struct {
	Span    lexer.Span
	Message string
}

func (problem *Error) Error() string {
	if problem == nil {
		return ErrInvalidSyntax.Error()
	}
	return fmt.Sprintf("%s at %d:%d", problem.Message, problem.Span.Start.Line, problem.Span.Start.Column)
}

func (problem *Error) Unwrap() error { return ErrInvalidSyntax }

type parser struct {
	tokens    []lexer.Token
	index     int
	async     int
	generator int
	classes   []classScope
}

type classScope struct {
	prefix       string
	privateNames map[string]string
	superBinding string
}

func Parse(source string) (*ast.Script, error) {
	tokens, err := lexer.Lex(source)
	if err != nil {
		return nil, err
	}
	return ParseTokens(tokens)
}

func ParseTokens(tokens []lexer.Token) (*ast.Script, error) {
	if len(tokens) == 0 || tokens[len(tokens)-1].Kind != lexer.EOF {
		return nil, &Error{Message: "token stream must end with EOF", Span: lexer.Span{Start: lexer.Position{Line: 1, Column: 1}}}
	}
	input := &parser{tokens: tokens}
	body := make([]ast.Statement, 0)
	for !input.check(lexer.EOF) {
		statement, err := input.parseModuleItem()
		if err != nil {
			return nil, err
		}
		body = append(body, statement)
	}
	start := tokens[0].Span.Start
	end := tokens[len(tokens)-1].Span.End
	return &ast.Script{Base: ast.Base{Range: lexer.Span{Start: start, End: end}}, Body: body}, nil
}

func (input *parser) parseModuleItem() (ast.Statement, error) {
	switch input.current().Kind {
	case lexer.Import:
		if input.peek(1).Kind == lexer.Dot || input.peek(1).Kind == lexer.LeftParen {
			return input.parseExpressionStatement()
		}
		return input.parseImportDeclaration()
	case lexer.Export:
		return input.parseExportDeclaration()
	default:
		return input.parseStatement()
	}
}

func (input *parser) parseStatement() (ast.Statement, error) {
	if input.checkContextual("async") && input.peek(1).Kind == lexer.Function &&
		input.current().Span.End.Line == input.peek(1).Span.Start.Line {
		return input.parseAsyncFunctionDeclaration()
	}
	switch input.current().Kind {
	case lexer.Semicolon:
		token := input.advance()
		return &ast.EmptyStatement{Base: ast.Base{Range: token.Span}}, nil
	case lexer.LeftBrace:
		return input.parseBlock()
	case lexer.Let, lexer.Const, lexer.Var:
		return input.parseVariableDeclaration()
	case lexer.Function:
		return input.parseFunctionDeclaration()
	case lexer.Class:
		return input.parseClassDeclaration()
	case lexer.Return:
		return input.parseReturnStatement()
	case lexer.If:
		return input.parseIfStatement()
	case lexer.While:
		return input.parseWhileStatement()
	case lexer.Do:
		return input.parseDoWhileStatement()
	case lexer.For:
		return input.parseForStatement()
	case lexer.Switch:
		return input.parseSwitchStatement()
	case lexer.Break:
		return input.parseSimpleControlStatement(true)
	case lexer.Continue:
		return input.parseSimpleControlStatement(false)
	case lexer.Throw:
		return input.parseThrowStatement()
	case lexer.Try:
		return input.parseTryStatement()
	default:
		if input.check(lexer.Identifier) && input.peek(1).Kind == lexer.Colon {
			return input.parseLabeledStatement()
		}
		return input.parseExpressionStatement()
	}
}

func (input *parser) parseImportDeclaration() (ast.Statement, error) {
	start := input.advance()
	declaration := &ast.ImportDeclaration{}
	if input.check(lexer.String) {
		source := input.advance()
		declaration.Source = source.Text
		end, err := input.finishStatement()
		if err != nil {
			return nil, err
		}
		declaration.Base = ast.Base{Range: join(start.Span, end)}
		return declaration, nil
	}

	if input.check(lexer.Identifier) {
		local := input.advance()
		declaration.Specifiers = append(declaration.Specifiers, &ast.ImportSpecifier{
			Base: ast.Base{Range: local.Span}, Kind: ast.ImportDefault, Imported: "default", Local: identifier(local),
		})
		if !input.match(lexer.Comma) {
			if !input.matchContextual("from") {
				return nil, input.errorAt(input.current(), "expected 'from' after default import")
			}
			return input.finishImportDeclaration(start, declaration)
		}
	}

	if input.match(lexer.Star) {
		star := input.previous()
		if !input.matchContextual("as") {
			return nil, input.errorAt(input.current(), "expected 'as' after namespace import")
		}
		local, err := input.consume(lexer.Identifier, "expected namespace import binding")
		if err != nil {
			return nil, err
		}
		declaration.Specifiers = append(declaration.Specifiers, &ast.ImportSpecifier{
			Base: ast.Base{Range: join(star.Span, local.Span)}, Kind: ast.ImportNamespace, Imported: "*", Local: identifier(local),
		})
	} else if input.match(lexer.LeftBrace) {
		for !input.check(lexer.RightBrace) {
			imported, err := input.consumeModuleName("expected imported name")
			if err != nil {
				return nil, err
			}
			local := imported
			if input.matchContextual("as") {
				local, err = input.consume(lexer.Identifier, "expected local import binding after 'as'")
				if err != nil {
					return nil, err
				}
			}
			declaration.Specifiers = append(declaration.Specifiers, &ast.ImportSpecifier{
				Base: ast.Base{Range: join(imported.Span, local.Span)}, Kind: ast.ImportNamed, Imported: imported.Text, Local: identifier(local),
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
		}
		if _, err := input.consume(lexer.RightBrace, "expected '}' after named imports"); err != nil {
			return nil, err
		}
	} else if len(declaration.Specifiers) == 0 {
		return nil, input.errorAt(input.current(), "expected import clause or source String")
	} else {
		return nil, input.errorAt(input.current(), "expected namespace or named imports after ','")
	}
	if !input.matchContextual("from") {
		return nil, input.errorAt(input.current(), "expected 'from' after import clause")
	}
	return input.finishImportDeclaration(start, declaration)
}

func (input *parser) finishImportDeclaration(start lexer.Token, declaration *ast.ImportDeclaration) (ast.Statement, error) {
	source, err := input.consume(lexer.String, "expected module source String")
	if err != nil {
		return nil, err
	}
	declaration.Source = source.Text
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	declaration.Base = ast.Base{Range: join(start.Span, end)}
	return declaration, nil
}

func (input *parser) parseExportDeclaration() (ast.Statement, error) {
	start := input.advance()
	if input.match(lexer.Default) {
		var expression ast.Expression
		var err error
		if input.check(lexer.Function) {
			expression, err = input.parseFunctionExpression(input.advance())
		} else if input.check(lexer.Class) {
			expression, err = input.parseClassExpression(input.advance())
		} else {
			expression, err = input.parseAssignment()
		}
		if err != nil {
			return nil, err
		}
		end, err := input.finishStatement()
		if err != nil {
			return nil, err
		}
		return &ast.ExportDefaultDeclaration{
			Base: ast.Base{Range: join(start.Span, end)}, Expression: expression,
		}, nil
	}
	if input.check(lexer.Let) || input.check(lexer.Const) || input.check(lexer.Var) || input.check(lexer.Function) || input.check(lexer.Class) {
		declaration, err := input.parseStatement()
		if err != nil {
			return nil, err
		}
		return &ast.ExportNamedDeclaration{
			Base: ast.Base{Range: join(start.Span, declaration.Span())}, Declaration: declaration,
		}, nil
	}
	if input.match(lexer.Star) {
		exported := ""
		if input.matchContextual("as") {
			name, err := input.consumeModuleName("expected namespace export name")
			if err != nil {
				return nil, err
			}
			exported = name.Text
		}
		if !input.matchContextual("from") {
			return nil, input.errorAt(input.current(), "expected 'from' after star export")
		}
		source, err := input.consume(lexer.String, "expected module source String")
		if err != nil {
			return nil, err
		}
		end, err := input.finishStatement()
		if err != nil {
			return nil, err
		}
		return &ast.ExportAllDeclaration{
			Base: ast.Base{Range: join(start.Span, end)}, Exported: exported, Source: source.Text,
		}, nil
	}
	if !input.match(lexer.LeftBrace) {
		return nil, input.errorAt(input.current(), "expected export declaration or clause")
	}
	specifiers := make([]*ast.ExportSpecifier, 0)
	for !input.check(lexer.RightBrace) {
		local, err := input.consumeModuleName("expected local export name")
		if err != nil {
			return nil, err
		}
		exported := local
		if input.matchContextual("as") {
			exported, err = input.consumeModuleName("expected exported name after 'as'")
			if err != nil {
				return nil, err
			}
		}
		specifiers = append(specifiers, &ast.ExportSpecifier{
			Base: ast.Base{Range: join(local.Span, exported.Span)}, Local: local.Text, Exported: exported.Text,
		})
		if !input.match(lexer.Comma) {
			break
		}
		if input.check(lexer.RightBrace) {
			break
		}
	}
	if _, err := input.consume(lexer.RightBrace, "expected '}' after export clause"); err != nil {
		return nil, err
	}
	source := ""
	if input.matchContextual("from") {
		token, err := input.consume(lexer.String, "expected module source String")
		if err != nil {
			return nil, err
		}
		source = token.Text
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	return &ast.ExportNamedDeclaration{
		Base: ast.Base{Range: join(start.Span, end)}, Specifiers: specifiers, Source: source,
	}, nil
}

func (input *parser) consumeModuleName(message string) (lexer.Token, error) {
	if input.check(lexer.Identifier) || input.check(lexer.Default) {
		return input.advance(), nil
	}
	return lexer.Token{}, input.errorAt(input.current(), message)
}

func (input *parser) parseBlock() (*ast.BlockStatement, error) {
	open, err := input.consume(lexer.LeftBrace, "expected '{'")
	if err != nil {
		return nil, err
	}
	body := make([]ast.Statement, 0)
	for !input.check(lexer.RightBrace) && !input.check(lexer.EOF) {
		statement, err := input.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, statement)
	}
	close, err := input.consume(lexer.RightBrace, "expected '}' after block")
	if err != nil {
		return nil, err
	}
	return &ast.BlockStatement{Base: ast.Base{Range: join(open.Span, close.Span)}, Body: body}, nil
}

func (input *parser) parseVariableDeclaration() (ast.Statement, error) {
	declaration, err := input.parseVariableDeclarationTerminated(true)
	return declaration, err
}

func (input *parser) parseVariableDeclarationTerminated(terminated bool) (*ast.VariableDeclaration, error) {
	start := input.advance()
	kind := ast.VariableLet
	switch start.Kind {
	case lexer.Const:
		kind = ast.VariableConst
	case lexer.Var:
		kind = ast.VariableVar
	}
	declarations := make([]*ast.VariableDeclarator, 0, 1)
	for {
		var name *ast.Identifier
		var arrayPattern []*ast.Identifier
		var objectPattern []*ast.ObjectBindingProperty
		var pattern *ast.BindingPattern
		var bindingSpan lexer.Span
		if input.check(lexer.Identifier) {
			nameToken := input.advance()
			name = identifier(nameToken)
			bindingSpan = nameToken.Span
		} else if input.check(lexer.LeftBracket) || input.check(lexer.LeftBrace) {
			var err error
			pattern, err = input.parseBindingPattern("binding")
			if err != nil {
				return nil, err
			}
			bindingSpan = pattern.Span()
			arrayPattern, objectPattern = legacyBindingPattern(pattern)
		} else {
			return nil, input.errorAt(input.current(), "expected binding name")
		}
		var initializer ast.Expression
		end := bindingSpan
		var err error
		if input.match(lexer.Assign) {
			initializer, err = input.parseAssignment()
			if err != nil {
				return nil, err
			}
			end = initializer.Span()
		} else if (kind == ast.VariableConst || pattern != nil) &&
			!(!terminated && (input.check(lexer.In) || input.checkContextual("of"))) {
			return nil, input.errorAt(input.current(), "binding pattern requires an initializer")
		}
		declarations = append(declarations, &ast.VariableDeclarator{
			Base: ast.Base{Range: join(bindingSpan, end)}, Name: name, ArrayPattern: arrayPattern, ObjectPattern: objectPattern,
			Pattern: pattern, Init: initializer,
		})
		if !input.match(lexer.Comma) {
			break
		}
	}
	end := declarations[len(declarations)-1].Span()
	if terminated {
		var err error
		end, err = input.finishStatement()
		if err != nil {
			return nil, err
		}
	}
	return &ast.VariableDeclaration{
		Base: ast.Base{Range: join(start.Span, end)}, Kind: kind, Declarations: declarations,
	}, nil
}

func (input *parser) parseBindingPattern(label string) (*ast.BindingPattern, error) {
	if input.check(lexer.Identifier) {
		name := identifier(input.advance())
		return &ast.BindingPattern{Base: ast.Base{Range: name.Span()}, Name: name}, nil
	}
	if input.check(lexer.LeftBracket) {
		return input.parseArrayBindingPattern(label)
	}
	if input.check(lexer.LeftBrace) {
		return input.parseObjectBindingPattern(label)
	}
	return nil, input.errorAt(input.current(), "expected "+label+" name or pattern")
}

func (input *parser) parseObjectBindingPattern(label string) (*ast.BindingPattern, error) {
	open, err := input.consume(lexer.LeftBrace, "expected '{' before object binding pattern")
	if err != nil {
		return nil, err
	}
	elements := make([]*ast.ObjectBindingElement, 0)
	for !input.check(lexer.RightBrace) {
		if input.check(lexer.EOF) {
			return nil, input.errorAt(input.current(), "expected '}' after object binding pattern")
		}
		if input.check(lexer.Ellipsis) {
			spread := input.advance()
			target, err := input.parseBindingPattern("object rest binding")
			if err != nil {
				return nil, err
			}
			elements = append(elements, &ast.ObjectBindingElement{
				Base: ast.Base{Range: join(spread.Span, target.Span())}, Pattern: target, Rest: true,
			})
			if input.match(lexer.Comma) {
				return nil, input.errorAt(input.previous(), "object rest binding must be last")
			}
			break
		}
		key := input.advance()
		if !isPropertyName(key.Kind) && key.Kind != lexer.String && key.Kind != lexer.Number {
			return nil, input.errorAt(key, "object binding property requires a static key")
		}
		keyText := key.Text
		if key.Kind == lexer.Number {
			keyText = key.Lexeme
		}
		var target *ast.BindingPattern
		if input.match(lexer.Colon) {
			target, err = input.parseBindingPattern("object binding property")
			if err != nil {
				return nil, err
			}
		} else if key.Kind == lexer.Identifier {
			name := identifier(key)
			target = &ast.BindingPattern{Base: ast.Base{Range: name.Span()}, Name: name}
		} else {
			return nil, input.errorAt(input.current(), "expected ':' after object binding key")
		}
		var defaultValue ast.Expression
		if input.match(lexer.Assign) {
			defaultValue, err = input.parseAssignment()
			if err != nil {
				return nil, err
			}
		}
		end := target.Span()
		if defaultValue != nil {
			end = defaultValue.Span()
		}
		elements = append(elements, &ast.ObjectBindingElement{
			Base: ast.Base{Range: join(key.Span, end)}, Key: keyText, Pattern: target, Default: defaultValue,
		})
		if !input.match(lexer.Comma) {
			break
		}
	}
	close, err := input.consume(lexer.RightBrace, "expected '}' after object binding pattern")
	if err != nil {
		return nil, err
	}
	return &ast.BindingPattern{Base: ast.Base{Range: join(open.Span, close.Span)}, Object: elements}, nil
}

func (input *parser) parseArrayBindingPattern(label string) (*ast.BindingPattern, error) {
	open, err := input.consume(lexer.LeftBracket, "expected '[' before array binding pattern")
	if err != nil {
		return nil, err
	}
	elements := make([]*ast.BindingElement, 0)
	for !input.check(lexer.RightBracket) {
		if input.check(lexer.EOF) {
			return nil, input.errorAt(input.current(), "expected ']' after array binding pattern")
		}
		if input.match(lexer.Comma) {
			elements = append(elements, nil)
			continue
		}
		var spread lexer.Token
		rest := false
		if input.check(lexer.Ellipsis) {
			spread = input.advance()
			rest = true
		}
		target, err := input.parseBindingPattern("array binding")
		if err != nil {
			return nil, err
		}
		var defaultValue ast.Expression
		if !rest && input.match(lexer.Assign) {
			defaultValue, err = input.parseAssignment()
			if err != nil {
				return nil, err
			}
		}
		start := target.Span()
		if rest {
			start = spread.Span
		}
		end := target.Span()
		if defaultValue != nil {
			end = defaultValue.Span()
		}
		elements = append(elements, &ast.BindingElement{
			Base: ast.Base{Range: join(start, end)}, Pattern: target, Default: defaultValue, Rest: rest,
		})
		if rest {
			if input.match(lexer.Comma) {
				return nil, input.errorAt(input.previous(), "array rest binding must be last")
			}
			break
		}
		if !input.match(lexer.Comma) {
			break
		}
	}
	close, err := input.consume(lexer.RightBracket, "expected ']' after array binding pattern")
	if err != nil {
		return nil, err
	}
	return &ast.BindingPattern{Base: ast.Base{Range: join(open.Span, close.Span)}, Array: elements}, nil
}

func legacyBindingPattern(pattern *ast.BindingPattern) ([]*ast.Identifier, []*ast.ObjectBindingProperty) {
	if pattern == nil {
		return nil, nil
	}
	if pattern.Array != nil {
		legacy := make([]*ast.Identifier, len(pattern.Array))
		for index, element := range pattern.Array {
			if element != nil && element.Pattern != nil {
				legacy[index] = element.Pattern.Name
			}
		}
		return legacy, nil
	}
	if pattern.Object != nil {
		legacy := make([]*ast.ObjectBindingProperty, 0, len(pattern.Object))
		for _, element := range pattern.Object {
			if element == nil || element.Rest || element.Pattern == nil || element.Pattern.Name == nil {
				continue
			}
			legacy = append(legacy, &ast.ObjectBindingProperty{
				Base: element.Base, Key: element.Key, Binding: element.Pattern.Name, Default: element.Default,
			})
		}
		return nil, legacy
	}
	return nil, nil
}

func (input *parser) parseFunctionDeclaration() (ast.Statement, error) {
	start := input.advance()
	return input.parseFunctionDeclarationAfterStart(start, false)
}

func (input *parser) parseClassDeclaration() (ast.Statement, error) {
	start := input.advance()
	nameToken, err := input.consume(lexer.Identifier, "class declaration requires a name")
	if err != nil {
		return nil, err
	}
	class, err := input.parseClassTail(start, identifier(nameToken))
	if err != nil {
		return nil, err
	}
	return &ast.ClassDeclaration{
		Base: class.Base, Name: class.Name, SuperClass: class.SuperClass,
		SuperBinding: class.SuperBinding, Elements: class.Elements,
	}, nil
}

func (input *parser) parseClassExpression(start lexer.Token) (ast.Expression, error) {
	var name *ast.Identifier
	if input.check(lexer.Identifier) {
		name = identifier(input.advance())
	}
	return input.parseClassTail(start, name)
}

func (input *parser) parseClassTail(start lexer.Token, name *ast.Identifier) (*ast.ClassExpression, error) {
	var superClass ast.Expression
	var err error
	if input.match(lexer.Extends) {
		superClass, err = input.parseAssignment()
		if err != nil {
			return nil, err
		}
	}
	_, err = input.consume(lexer.LeftBrace, "expected '{' before class body")
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("\x00gossamer.class.%d", start.Span.Start.Offset)
	scope := classScope{prefix: prefix, privateNames: make(map[string]string)}
	if superClass != nil {
		scope.superBinding = prefix + ".super"
	}
	input.classes = append(input.classes, scope)
	defer func() { input.classes = input.classes[:len(input.classes)-1] }()

	elements := make([]*ast.ClassElement, 0)
	seenConstructor := false
	seenPrivate := make(map[string]lexer.Span)
	for !input.check(lexer.RightBrace) {
		if input.check(lexer.EOF) {
			return nil, input.errorAt(input.current(), "expected '}' after class body")
		}
		if input.match(lexer.Semicolon) {
			continue
		}
		element, err := input.parseClassElement(seenPrivate)
		if err != nil {
			return nil, err
		}
		if element.Kind == ast.ClassConstructor {
			if seenConstructor {
				return nil, input.errorAt(input.previous(), "class may only have one constructor")
			}
			seenConstructor = true
		}
		elements = append(elements, element)
	}
	close := input.advance()
	return &ast.ClassExpression{
		Base: ast.Base{Range: join(start.Span, close.Span)}, Name: name,
		SuperClass: superClass, SuperBinding: scope.superBinding, Elements: elements,
	}, nil
}

func (input *parser) parseClassElement(seenPrivate map[string]lexer.Span) (*ast.ClassElement, error) {
	start := input.current()
	static := false
	if input.checkContextual("static") && input.peek(1).Kind != lexer.LeftParen &&
		input.peek(1).Kind != lexer.Assign && input.peek(1).Kind != lexer.Semicolon && input.peek(1).Kind != lexer.RightBrace {
		input.advance()
		static = true
	}

	async := false
	if input.checkContextual("async") && input.peek(1).Kind != lexer.Assign && input.peek(1).Kind != lexer.Semicolon &&
		input.current().Span.End.Line == input.peek(1).Span.Start.Line {
		input.advance()
		async = true
	}
	generator := input.match(lexer.Star)
	accessor := ast.ClassMethod
	if !async && !generator && input.check(lexer.Identifier) && (input.current().Text == "get" || input.current().Text == "set") &&
		input.classKeyStartsAt(1) {
		candidate := input.advance()
		if candidate.Text == "get" {
			accessor = ast.ClassGetter
		} else {
			accessor = ast.ClassSetter
		}
	}

	key, keyExpression, computed, private, keySpan, err := input.parseClassKey()
	if err != nil {
		return nil, err
	}
	if private {
		if previous, duplicate := seenPrivate[key]; duplicate {
			return nil, input.errorAt(start, fmt.Sprintf("private class name already declared at %d:%d", previous.Start.Line, previous.Start.Column))
		}
		seenPrivate[key] = keySpan
	}

	if input.check(lexer.LeftParen) {
		parameters, body, end, err := input.parseFunctionTailMode(async, generator)
		if err != nil {
			return nil, err
		}
		kind := accessor
		if !computed && !private && !static && key == "constructor" {
			if async || generator || accessor != ast.ClassMethod {
				return nil, input.errorAt(start, "class constructor cannot be async, a generator, or an accessor")
			}
			kind = ast.ClassConstructor
		}
		if accessor == ast.ClassGetter && len(parameters) != 0 {
			return nil, input.errorAt(start, "class getter must not have parameters")
		}
		if accessor == ast.ClassSetter && len(parameters) != 1 {
			return nil, input.errorAt(start, "class setter must have exactly one parameter")
		}
		functionName := key
		if computed {
			functionName = ""
		}
		var functionIdentifier *ast.Identifier
		if functionName != "" {
			functionIdentifier = &ast.Identifier{Base: ast.Base{Range: keySpan}, Name: functionName}
		}
		input.match(lexer.Semicolon)
		return &ast.ClassElement{
			Base: ast.Base{Range: join(start.Span, end)}, Kind: kind, Key: key,
			KeyExpression: keyExpression, Computed: computed, Private: private, Static: static,
			Function: &ast.FunctionExpression{
				Base: ast.Base{Range: join(start.Span, end)}, Name: functionIdentifier,
				Parameters: parameters, Body: body, Async: async, Generator: generator,
			},
		}, nil
	}

	if async || generator || accessor != ast.ClassMethod {
		return nil, input.errorAt(start, "class field cannot use a method modifier")
	}
	var value ast.Expression
	end := keySpan
	if input.match(lexer.Assign) {
		value, err = input.parseAssignment()
		if err != nil {
			return nil, err
		}
		end = value.Span()
	}
	if input.match(lexer.Semicolon) {
		end = input.previous().Span
	} else if !input.check(lexer.RightBrace) && input.current().Span.Start.Line <= end.End.Line {
		return nil, input.errorAt(input.current(), "expected ';' after class field")
	}
	return &ast.ClassElement{
		Base: ast.Base{Range: join(start.Span, end)}, Kind: ast.ClassField, Key: key,
		KeyExpression: keyExpression, Value: value, Computed: computed, Private: private, Static: static,
	}, nil
}

func (input *parser) classKeyStartsAt(distance int) bool {
	token := input.peek(distance)
	if token.Kind == lexer.LeftBracket {
		return true
	}
	if token.Kind == lexer.PrivateIdentifier || token.Kind == lexer.String || token.Kind == lexer.Number || isPropertyName(token.Kind) {
		return input.peek(distance+1).Kind == lexer.LeftParen
	}
	return false
}

func (input *parser) parseClassKey() (string, ast.Expression, bool, bool, lexer.Span, error) {
	if input.match(lexer.LeftBracket) {
		open := input.previous()
		expression, err := input.parseExpression()
		if err != nil {
			return "", nil, false, false, lexer.Span{}, err
		}
		close, err := input.consume(lexer.RightBracket, "expected ']' after computed class key")
		if err != nil {
			return "", nil, false, false, lexer.Span{}, err
		}
		return "", expression, true, false, join(open.Span, close.Span), nil
	}
	token := input.advance()
	if token.Kind == lexer.PrivateIdentifier {
		name, err := input.privateName(token)
		return name, nil, false, true, token.Span, err
	}
	if !isPropertyName(token.Kind) && token.Kind != lexer.String && token.Kind != lexer.Number {
		return "", nil, false, false, lexer.Span{}, input.errorAt(token, "expected class element name")
	}
	name := token.Text
	if token.Kind == lexer.Number {
		name = token.Lexeme
	}
	return name, nil, false, false, token.Span, nil
}

func (input *parser) privateName(token lexer.Token) (string, error) {
	if len(input.classes) == 0 {
		return "", input.errorAt(token, "private identifier is only valid inside a class")
	}
	scope := &input.classes[len(input.classes)-1]
	if name, exists := scope.privateNames[token.Text]; exists {
		return name, nil
	}
	name := scope.prefix + ".private." + token.Text
	scope.privateNames[token.Text] = name
	return name, nil
}

func (input *parser) parseAsyncFunctionDeclaration() (ast.Statement, error) {
	start := input.advance()
	if _, err := input.consume(lexer.Function, "expected function after async"); err != nil {
		return nil, err
	}
	return input.parseFunctionDeclarationAfterStart(start, true)
}

func (input *parser) parseFunctionDeclarationAfterStart(start lexer.Token, async bool) (ast.Statement, error) {
	generator := input.match(lexer.Star)
	nameToken, err := input.consume(lexer.Identifier, "function declaration requires a name")
	if err != nil {
		return nil, err
	}
	parameters, body, end, err := input.parseFunctionTailMode(async, generator)
	if err != nil {
		return nil, err
	}
	return &ast.FunctionDeclaration{
		Base: ast.Base{Range: join(start.Span, end)}, Name: identifier(nameToken), Parameters: parameters, Body: body, Async: async, Generator: generator,
	}, nil
}

func (input *parser) parseFunctionTail() ([]*ast.Identifier, *ast.BlockStatement, lexer.Span, error) {
	return input.parseFunctionTailMode(false, false)
}

func (input *parser) parseFunctionTailAsync(async bool) ([]*ast.Identifier, *ast.BlockStatement, lexer.Span, error) {
	return input.parseFunctionTailMode(async, false)
}

func (input *parser) parseFunctionTailMode(async, generator bool) ([]*ast.Identifier, *ast.BlockStatement, lexer.Span, error) {
	if _, err := input.consume(lexer.LeftParen, "expected '(' before parameters"); err != nil {
		return nil, nil, lexer.Span{}, err
	}
	parameters := make([]*ast.Identifier, 0)
	defaults := make([]ast.Expression, 0)
	patterns := make([]*ast.VariableDeclaration, 0)
	var rest []ast.Statement
	if !input.check(lexer.RightParen) {
		for {
			if input.match(lexer.Ellipsis) {
				spread := input.previous()
				parameter, pattern, err := input.parseParameterBinding("rest parameter")
				if err != nil {
					return nil, nil, lexer.Span{}, err
				}
				if input.check(lexer.Assign) {
					return nil, nil, lexer.Span{}, input.errorAt(input.current(), "rest parameter cannot have a default")
				}
				rest = restParameterPrologue(parameter, pattern, len(parameters), join(spread.Span, parameter.Span()))
				if input.match(lexer.Comma) {
					return nil, nil, lexer.Span{}, input.errorAt(input.previous(), "rest parameter must be last")
				}
				break
			}
			parameter, pattern, err := input.parseParameterBinding("parameter")
			if err != nil {
				return nil, nil, lexer.Span{}, err
			}
			parameters = append(parameters, parameter)
			patterns = append(patterns, pattern)
			var defaultValue ast.Expression
			if input.match(lexer.Assign) {
				defaultValue, err = input.parseAssignment()
				if err != nil {
					return nil, nil, lexer.Span{}, err
				}
			}
			defaults = append(defaults, defaultValue)
			if !input.match(lexer.Comma) {
				break
			}
		}
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after parameters"); err != nil {
		return nil, nil, lexer.Span{}, err
	}
	previousAsync := input.async
	previousGenerator := input.generator
	if async {
		input.async = 1
	} else {
		input.async = 0
	}
	if generator {
		input.generator = 1
	} else {
		input.generator = 0
	}
	defer func() {
		input.async = previousAsync
		input.generator = previousGenerator
	}()
	body, err := input.parseBlock()
	if err != nil {
		return nil, nil, lexer.Span{}, err
	}
	prologue := append(parameterPrologue(parameters, defaults, patterns), rest...)
	body.Body = append(prologue, body.Body...)
	return parameters, body, body.Span(), nil
}

func (input *parser) parseParameterBinding(label string) (*ast.Identifier, *ast.VariableDeclaration, error) {
	if input.check(lexer.Identifier) {
		return identifier(input.advance()), nil, nil
	}
	var declarator ast.VariableDeclarator
	var span lexer.Span
	if input.check(lexer.LeftBracket) || input.check(lexer.LeftBrace) {
		pattern, err := input.parseBindingPattern(label)
		if err != nil {
			return nil, nil, err
		}
		declarator.Pattern = pattern
		declarator.ArrayPattern, declarator.ObjectPattern = legacyBindingPattern(pattern)
		span = pattern.Span()
	} else {
		return nil, nil, input.errorAt(input.current(), "expected "+label+" name")
	}
	temporary := &ast.Identifier{
		Base: ast.Base{Range: span}, Name: fmt.Sprintf("\x00gossamer.%s.pattern.%d", label, span.Start.Offset),
	}
	declarator.Base = ast.Base{Range: span}
	declarator.Init = temporary
	declaration := &ast.VariableDeclaration{
		Base: ast.Base{Range: span}, Kind: ast.VariableConst, Declarations: []*ast.VariableDeclarator{&declarator},
	}
	return temporary, declaration, nil
}

func parameterPrologue(parameters []*ast.Identifier, defaults []ast.Expression, patterns []*ast.VariableDeclaration) []ast.Statement {
	prologue := make([]ast.Statement, 0, len(defaults)+len(patterns))
	for index, defaultValue := range defaults {
		if defaultValue != nil {
			parameter := parameters[index]
			test := &ast.BinaryExpression{
				Base:     ast.Base{Range: join(parameter.Span(), defaultValue.Span())},
				Operator: lexer.StrictEqual,
				Left:     &ast.UnaryExpression{Base: ast.Base{Range: parameter.Span()}, Operator: lexer.Typeof, Argument: parameter},
				Right:    &ast.StringLiteral{Base: ast.Base{Range: parameter.Span()}, Value: "undefined"},
			}
			assignment := &ast.AssignmentExpression{
				Base:     ast.Base{Range: join(parameter.Span(), defaultValue.Span())},
				Operator: lexer.Assign, Left: parameter, Right: defaultValue,
			}
			consequent := &ast.ExpressionStatement{Base: ast.Base{Range: assignment.Span()}, Expression: assignment}
			prologue = append(prologue, &ast.IfStatement{
				Base: ast.Base{Range: join(parameter.Span(), defaultValue.Span())}, Test: test, Consequent: consequent,
			})
		}
		if index < len(patterns) && patterns[index] != nil {
			prologue = append(prologue, patterns[index])
		}
	}
	return prologue
}

func restParameterPrologue(parameter *ast.Identifier, pattern *ast.VariableDeclaration, index int, span lexer.Span) []ast.Statement {
	array := &ast.Identifier{Base: ast.Base{Range: span}, Name: "Array"}
	prototype := &ast.MemberExpression{
		Base: ast.Base{Range: span}, Object: array,
		Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: "prototype"},
	}
	slice := &ast.MemberExpression{
		Base: ast.Base{Range: span}, Object: prototype,
		Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: "slice"},
	}
	call := &ast.MemberExpression{
		Base: ast.Base{Range: span}, Object: slice,
		Property: &ast.Identifier{Base: ast.Base{Range: span}, Name: "call"},
	}
	rest := &ast.CallExpression{
		Base: ast.Base{Range: span}, Callee: call,
		Arguments: []ast.Expression{
			&ast.Identifier{Base: ast.Base{Range: span}, Name: "arguments"},
			&ast.NumberLiteral{Base: ast.Base{Range: span}, Value: float64(index)},
		},
	}
	declaration := &ast.VariableDeclaration{
		Base: ast.Base{Range: span}, Kind: ast.VariableLet,
		Declarations: []*ast.VariableDeclarator{{
			Base: ast.Base{Range: span}, Name: parameter, Init: rest,
		}},
	}
	result := []ast.Statement{declaration}
	if pattern != nil {
		result = append(result, pattern)
	}
	return result
}

func (input *parser) parseReturnStatement() (ast.Statement, error) {
	start := input.advance()
	var argument ast.Expression
	if !input.statementEndedAfter(start) {
		var err error
		argument, err = input.parseExpression()
		if err != nil {
			return nil, err
		}
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	return &ast.ReturnStatement{Base: ast.Base{Range: join(start.Span, end)}, Argument: argument}, nil
}

func (input *parser) parseIfStatement() (ast.Statement, error) {
	start := input.advance()
	if _, err := input.consume(lexer.LeftParen, "expected '(' after if"); err != nil {
		return nil, err
	}
	test, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after if condition"); err != nil {
		return nil, err
	}
	consequent, err := input.parseStatement()
	if err != nil {
		return nil, err
	}
	end := consequent.Span()
	var alternate ast.Statement
	if input.match(lexer.Else) {
		alternate, err = input.parseStatement()
		if err != nil {
			return nil, err
		}
		end = alternate.Span()
	}
	return &ast.IfStatement{
		Base: ast.Base{Range: join(start.Span, end)}, Test: test, Consequent: consequent, Alternate: alternate,
	}, nil
}

func (input *parser) parseWhileStatement() (ast.Statement, error) {
	start := input.advance()
	if _, err := input.consume(lexer.LeftParen, "expected '(' after while"); err != nil {
		return nil, err
	}
	test, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after while condition"); err != nil {
		return nil, err
	}
	body, err := input.parseStatement()
	if err != nil {
		return nil, err
	}
	return &ast.WhileStatement{Base: ast.Base{Range: join(start.Span, body.Span())}, Test: test, Body: body}, nil
}

func (input *parser) parseDoWhileStatement() (ast.Statement, error) {
	start := input.advance()
	body, err := input.parseStatement()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.While, "expected 'while' after do body"); err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.LeftParen, "expected '(' after while"); err != nil {
		return nil, err
	}
	test, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after do-while condition"); err != nil {
		return nil, err
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	return &ast.DoWhileStatement{Base: ast.Base{Range: join(start.Span, end)}, Body: body, Test: test}, nil
}

func (input *parser) parseForStatement() (ast.Statement, error) {
	start := input.advance()
	if _, err := input.consume(lexer.LeftParen, "expected '(' after for"); err != nil {
		return nil, err
	}
	var declaration *ast.VariableDeclaration
	var initializer ast.Expression
	var err error
	if input.check(lexer.Let) || input.check(lexer.Const) || input.check(lexer.Var) {
		declaration, err = input.parseVariableDeclarationTerminated(false)
	} else if input.check(lexer.Identifier) && (input.peek(1).Kind == lexer.In || input.peek(1).Kind == lexer.Identifier && input.peek(1).Text == "of") {
		initializer = identifier(input.advance())
	} else if !input.check(lexer.Semicolon) {
		initializer, err = input.parseExpression()
	}
	if err != nil {
		return nil, err
	}
	if input.match(lexer.In) || input.matchContextual("of") {
		of := input.previous().Kind == lexer.Identifier
		if declaration != nil && len(declaration.Declarations) != 1 {
			return nil, input.errorAt(input.previous(), "for-in/of declaration requires one binding")
		}
		if declaration == nil && !ast.IsAssignmentTarget(initializer) {
			return nil, input.errorAt(input.previous(), "for-in/of left side is not assignable")
		}
		right, err := input.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := input.consume(lexer.RightParen, "expected ')' after for-in/of header"); err != nil {
			return nil, err
		}
		body, err := input.parseStatement()
		if err != nil {
			return nil, err
		}
		return &ast.ForInStatement{
			Base: ast.Base{Range: join(start.Span, body.Span())}, LeftDeclaration: declaration,
			LeftExpression: initializer, Right: right, Body: body, Of: of,
		}, nil
	}
	if _, err := input.consume(lexer.Semicolon, "expected ';' after for initializer"); err != nil {
		return nil, err
	}
	var test ast.Expression
	if !input.check(lexer.Semicolon) {
		test, err = input.parseExpression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := input.consume(lexer.Semicolon, "expected ';' after for condition"); err != nil {
		return nil, err
	}
	var update ast.Expression
	if !input.check(lexer.RightParen) {
		update, err = input.parseExpression()
		if err != nil {
			return nil, err
		}
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after for clauses"); err != nil {
		return nil, err
	}
	body, err := input.parseStatement()
	if err != nil {
		return nil, err
	}
	return &ast.ForStatement{
		Base: ast.Base{Range: join(start.Span, body.Span())}, InitDeclaration: declaration,
		InitExpression: initializer, Test: test, Update: update, Body: body,
	}, nil
}

func (input *parser) parseSwitchStatement() (ast.Statement, error) {
	start := input.advance()
	if _, err := input.consume(lexer.LeftParen, "expected '(' after switch"); err != nil {
		return nil, err
	}
	discriminant, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.RightParen, "expected ')' after switch value"); err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.LeftBrace, "expected '{' after switch"); err != nil {
		return nil, err
	}
	cases := make([]*ast.SwitchCase, 0)
	seenDefault := false
	for !input.check(lexer.RightBrace) && !input.check(lexer.EOF) {
		caseStart := input.current()
		var test ast.Expression
		if input.match(lexer.Case) {
			test, err = input.parseExpression()
			if err != nil {
				return nil, err
			}
		} else if input.match(lexer.Default) {
			if seenDefault {
				return nil, input.errorAt(caseStart, "switch has more than one default")
			}
			seenDefault = true
		} else {
			return nil, input.errorAt(caseStart, "expected case or default in switch")
		}
		colon, err := input.consume(lexer.Colon, "expected ':' after switch case")
		if err != nil {
			return nil, err
		}
		consequent := make([]ast.Statement, 0)
		end := colon.Span
		for !input.check(lexer.Case) && !input.check(lexer.Default) && !input.check(lexer.RightBrace) && !input.check(lexer.EOF) {
			statement, err := input.parseStatement()
			if err != nil {
				return nil, err
			}
			consequent = append(consequent, statement)
			end = statement.Span()
		}
		cases = append(cases, &ast.SwitchCase{Base: ast.Base{Range: join(caseStart.Span, end)}, Test: test, Consequent: consequent})
	}
	close, err := input.consume(lexer.RightBrace, "expected '}' after switch")
	if err != nil {
		return nil, err
	}
	return &ast.SwitchStatement{Base: ast.Base{Range: join(start.Span, close.Span)}, Discriminant: discriminant, Cases: cases}, nil
}

func (input *parser) parseLabeledStatement() (ast.Statement, error) {
	labelToken := input.advance()
	input.advance() // colon
	body, err := input.parseStatement()
	if err != nil {
		return nil, err
	}
	return &ast.LabeledStatement{Base: ast.Base{Range: join(labelToken.Span, body.Span())}, Label: identifier(labelToken), Body: body}, nil
}

func (input *parser) parseSimpleControlStatement(isBreak bool) (ast.Statement, error) {
	start := input.advance()
	var label *ast.Identifier
	if input.check(lexer.Identifier) && input.current().Span.Start.Line == start.Span.End.Line {
		label = identifier(input.advance())
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	base := ast.Base{Range: join(start.Span, end)}
	if isBreak {
		return &ast.BreakStatement{Base: base, Label: label}, nil
	}
	return &ast.ContinueStatement{Base: base, Label: label}, nil
}

func (input *parser) parseThrowStatement() (ast.Statement, error) {
	start := input.advance()
	if input.statementEndedAfter(start) || input.current().Span.Start.Line > start.Span.End.Line {
		return nil, input.errorAt(input.current(), "throw requires an expression on the same line")
	}
	argument, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	return &ast.ThrowStatement{Base: ast.Base{Range: join(start.Span, end)}, Argument: argument}, nil
}

func (input *parser) parseTryStatement() (ast.Statement, error) {
	start := input.advance()
	body, err := input.parseBlock()
	if err != nil {
		return nil, err
	}
	end := body.Span()
	var handler *ast.CatchClause
	if input.match(lexer.Catch) {
		catchStart := input.previous().Span
		var parameter *ast.Identifier
		if input.match(lexer.LeftParen) {
			name, err := input.consume(lexer.Identifier, "expected catch binding")
			if err != nil {
				return nil, err
			}
			parameter = identifier(name)
			if _, err := input.consume(lexer.RightParen, "expected ')' after catch binding"); err != nil {
				return nil, err
			}
		}
		catchBody, err := input.parseBlock()
		if err != nil {
			return nil, err
		}
		handler = &ast.CatchClause{
			Base: ast.Base{Range: join(catchStart, catchBody.Span())}, Parameter: parameter, Body: catchBody,
		}
		end = catchBody.Span()
	}
	var finalizer *ast.BlockStatement
	if input.match(lexer.Finally) {
		finalizer, err = input.parseBlock()
		if err != nil {
			return nil, err
		}
		end = finalizer.Span()
	}
	if handler == nil && finalizer == nil {
		return nil, input.errorAt(input.current(), "try requires catch or finally")
	}
	return &ast.TryStatement{
		Base: ast.Base{Range: join(start.Span, end)}, Body: body, Handler: handler, Finalizer: finalizer,
	}, nil
}

func (input *parser) parseExpressionStatement() (ast.Statement, error) {
	expression, err := input.parseExpression()
	if err != nil {
		return nil, err
	}
	end, err := input.finishStatement()
	if err != nil {
		return nil, err
	}
	return &ast.ExpressionStatement{Base: ast.Base{Range: join(expression.Span(), end)}, Expression: expression}, nil
}

func (input *parser) parseExpression() (ast.Expression, error) {
	first, err := input.parseAssignment()
	if err != nil || !input.match(lexer.Comma) {
		return first, err
	}
	expressions := []ast.Expression{first}
	for {
		next, err := input.parseAssignment()
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, next)
		if !input.match(lexer.Comma) {
			break
		}
	}
	return &ast.SequenceExpression{
		Base: ast.Base{Range: join(first.Span(), expressions[len(expressions)-1].Span())}, Expressions: expressions,
	}, nil
}

func (input *parser) parseAssignment() (ast.Expression, error) {
	if input.isArrowFunctionStart() {
		return input.parseArrowFunction()
	}
	left, err := input.parseConditional()
	if err != nil {
		return nil, err
	}
	if !input.match(
		lexer.Assign, lexer.PlusAssign, lexer.MinusAssign, lexer.StarAssign, lexer.SlashAssign,
		lexer.PercentAssign, lexer.AmpersandAssign, lexer.PipeAssign, lexer.CaretAssign,
		lexer.ShiftLeftAssign, lexer.ShiftRightAssign, lexer.UnsignedShiftRightAssign, lexer.StarStarAssign,
		lexer.AndAndAssign, lexer.OrOrAssign, lexer.NullishAssign,
	) {
		return left, nil
	}
	operator := input.previous()
	if !ast.IsAssignmentTarget(left) {
		return nil, input.errorAt(operator, "invalid assignment target")
	}
	right, err := input.parseAssignment()
	if err != nil {
		return nil, err
	}
	return &ast.AssignmentExpression{
		Base: ast.Base{Range: join(left.Span(), right.Span())}, Operator: operator.Kind, Left: left, Right: right,
	}, nil
}

func (input *parser) parseConditional() (ast.Expression, error) {
	test, err := input.parseNullish()
	if err != nil || !input.match(lexer.Question) {
		return test, err
	}
	consequent, err := input.parseAssignment()
	if err != nil {
		return nil, err
	}
	if _, err := input.consume(lexer.Colon, "expected ':' in conditional expression"); err != nil {
		return nil, err
	}
	alternate, err := input.parseAssignment()
	if err != nil {
		return nil, err
	}
	return &ast.ConditionalExpression{
		Base: ast.Base{Range: join(test.Span(), alternate.Span())}, Test: test, Consequent: consequent, Alternate: alternate,
	}, nil
}

func (input *parser) parseNullish() (ast.Expression, error) {
	return input.parseBinary(input.parseLogicalOr, lexer.Nullish)
}

func (input *parser) parseLogicalOr() (ast.Expression, error) {
	return input.parseBinary(input.parseLogicalAnd, lexer.OrOr)
}

func (input *parser) parseLogicalAnd() (ast.Expression, error) {
	return input.parseBinary(input.parseBitwiseOr, lexer.AndAnd)
}

func (input *parser) parseBitwiseOr() (ast.Expression, error) {
	return input.parseBinary(input.parseBitwiseXor, lexer.Pipe)
}

func (input *parser) parseBitwiseXor() (ast.Expression, error) {
	return input.parseBinary(input.parseBitwiseAnd, lexer.Caret)
}

func (input *parser) parseBitwiseAnd() (ast.Expression, error) {
	return input.parseBinary(input.parseEquality, lexer.Ampersand)
}

func (input *parser) parseEquality() (ast.Expression, error) {
	return input.parseBinary(input.parseRelational, lexer.StrictEqual, lexer.StrictNotEqual, lexer.EqualEqual, lexer.BangEqual)
}

func (input *parser) parseRelational() (ast.Expression, error) {
	return input.parseBinary(input.parseShift, lexer.Less, lexer.LessEqual, lexer.Greater, lexer.GreaterEqual, lexer.In, lexer.Instanceof)
}

func (input *parser) parseShift() (ast.Expression, error) {
	return input.parseBinary(input.parseAdditive, lexer.ShiftLeft, lexer.ShiftRight, lexer.UnsignedShiftRight)
}

func (input *parser) parseAdditive() (ast.Expression, error) {
	return input.parseBinary(input.parseMultiplicative, lexer.Plus, lexer.Minus)
}

func (input *parser) parseMultiplicative() (ast.Expression, error) {
	return input.parseBinary(input.parseExponentiation, lexer.Star, lexer.Slash, lexer.Percent)
}

func (input *parser) parseExponentiation() (ast.Expression, error) {
	left, err := input.parseUnary()
	if err != nil || !input.match(lexer.StarStar) {
		return left, err
	}
	right, err := input.parseExponentiation()
	if err != nil {
		return nil, err
	}
	return &ast.BinaryExpression{
		Base: ast.Base{Range: join(left.Span(), right.Span())}, Operator: lexer.StarStar, Left: left, Right: right,
	}, nil
}

func (input *parser) parseBinary(next func() (ast.Expression, error), operators ...lexer.Kind) (ast.Expression, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for contains(operators, input.current().Kind) {
		operator := input.advance()
		right, err := next()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpression{
			Base: ast.Base{Range: join(left.Span(), right.Span())}, Operator: operator.Kind, Left: left, Right: right,
		}
	}
	return left, nil
}

func (input *parser) parseUnary() (ast.Expression, error) {
	if input.generator > 0 && input.checkContextual("yield") {
		start := input.advance()
		if input.current().Span.Start.Line > start.Span.End.Line || input.check(lexer.Semicolon) || input.check(lexer.RightBrace) || input.check(lexer.EOF) {
			return &ast.YieldExpression{Base: ast.Base{Range: start.Span}}, nil
		}
		argument, err := input.parseAssignment()
		if err != nil {
			return nil, err
		}
		return &ast.YieldExpression{Base: ast.Base{Range: join(start.Span, argument.Span())}, Argument: argument}, nil
	}
	if input.async > 0 && input.checkContextual("await") {
		start := input.advance()
		argument, err := input.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.AwaitExpression{Base: ast.Base{Range: join(start.Span, argument.Span())}, Argument: argument}, nil
	}
	if input.match(lexer.Bang, lexer.Minus, lexer.Plus, lexer.Typeof, lexer.Delete, lexer.Void, lexer.Tilde) {
		operator := input.previous()
		argument, err := input.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpression{
			Base: ast.Base{Range: join(operator.Span, argument.Span())}, Operator: operator.Kind, Argument: argument,
		}, nil
	}
	if input.match(lexer.PlusPlus, lexer.MinusMinus) {
		operator := input.previous()
		argument, err := input.parseUnary()
		if err != nil {
			return nil, err
		}
		if !ast.IsAssignmentTarget(argument) {
			return nil, input.errorAt(operator, "update operand is not assignable")
		}
		return &ast.UpdateExpression{
			Base: ast.Base{Range: join(operator.Span, argument.Span())}, Operator: operator.Kind, Argument: argument, Prefix: true,
		}, nil
	}
	if input.match(lexer.New) {
		return input.parseNewExpression(input.previous())
	}
	return input.parsePostfix()
}

func (input *parser) parseNewExpression(start lexer.Token) (ast.Expression, error) {
	callee, err := input.parsePrimary()
	if err != nil {
		return nil, err
	}
	callee, err = input.parseMembers(callee)
	if err != nil {
		return nil, err
	}
	arguments := []ast.Expression(nil)
	end := callee.Span()
	if input.match(lexer.LeftParen) {
		arguments, end, err = input.parseArgumentsAfterOpen()
		if err != nil {
			return nil, err
		}
	}
	expression := &ast.NewExpression{Base: ast.Base{Range: join(start.Span, end)}, Callee: callee, Arguments: arguments}
	return input.parsePostfixSuffix(expression)
}

func (input *parser) parsePostfix() (ast.Expression, error) {
	expression, err := input.parsePrimary()
	if err != nil {
		return nil, err
	}
	expression, err = input.parsePostfixSuffix(expression)
	if err != nil {
		return nil, err
	}
	if input.check(lexer.PlusPlus) || input.check(lexer.MinusMinus) {
		operator := input.current()
		if operator.Span.Start.Line == expression.Span().End.Line {
			input.advance()
			if !ast.IsAssignmentTarget(expression) {
				return nil, input.errorAt(operator, "update operand is not assignable")
			}
			expression = &ast.UpdateExpression{
				Base: ast.Base{Range: join(expression.Span(), operator.Span)}, Operator: operator.Kind, Argument: expression,
			}
		}
	}
	return expression, nil
}

func (input *parser) parsePostfixSuffix(expression ast.Expression) (ast.Expression, error) {
	var err error
	for {
		expression, err = input.parseMembers(expression)
		if err != nil {
			return nil, err
		}
		optional := false
		if input.check(lexer.OptionalChain) && input.peek(1).Kind == lexer.LeftParen {
			input.advance()
			input.advance()
			optional = true
		} else if input.match(lexer.LeftParen) {
			// ordinary call
		} else {
			break
		}
		{
			arguments, end, err := input.parseArgumentsAfterOpen()
			if err != nil {
				return nil, err
			}
			expression = &ast.CallExpression{
				Base: ast.Base{Range: join(expression.Span(), end)}, Callee: expression, Arguments: arguments, Optional: optional,
			}
			continue
		}
	}
	return expression, nil
}

func (input *parser) parseMembers(expression ast.Expression) (ast.Expression, error) {
	for {
		optional := input.check(lexer.OptionalChain) && input.peek(1).Kind != lexer.LeftParen
		if optional {
			input.advance()
		}
		if optional || input.match(lexer.Dot) {
			if optional && input.match(lexer.LeftBracket) {
				property, err := input.parseExpression()
				if err != nil {
					return nil, err
				}
				close, err := input.consume(lexer.RightBracket, "expected ']' after computed optional property")
				if err != nil {
					return nil, err
				}
				expression = &ast.MemberExpression{
					Base: ast.Base{Range: join(expression.Span(), close.Span)}, Object: expression, Property: property, Computed: true, Optional: true,
				}
				continue
			}
			property := input.current()
			if property.Kind == lexer.PrivateIdentifier {
				input.advance()
				name, err := input.privateName(property)
				if err != nil {
					return nil, err
				}
				propertyExpression := &ast.Identifier{Base: ast.Base{Range: property.Span}, Name: name}
				expression = &ast.MemberExpression{
					Base: ast.Base{Range: join(expression.Span(), property.Span)}, Object: expression, Property: propertyExpression, Optional: optional,
				}
				continue
			}
			if !isPropertyName(property.Kind) {
				return nil, input.errorAt(property, "expected property name after '.'")
			}
			input.advance()
			propertyExpression := identifier(property)
			expression = &ast.MemberExpression{
				Base: ast.Base{Range: join(expression.Span(), property.Span)}, Object: expression, Property: propertyExpression, Optional: optional,
			}
			continue
		}
		if input.match(lexer.LeftBracket) {
			property, err := input.parseExpression()
			if err != nil {
				return nil, err
			}
			close, err := input.consume(lexer.RightBracket, "expected ']' after computed property")
			if err != nil {
				return nil, err
			}
			expression = &ast.MemberExpression{
				Base: ast.Base{Range: join(expression.Span(), close.Span)}, Object: expression, Property: property, Computed: true,
			}
			continue
		}
		return expression, nil
	}
}

func (input *parser) isArrowFunctionStart() bool {
	if input.check(lexer.Identifier) {
		if input.peek(1).Kind == lexer.Arrow {
			return true
		}
		if input.checkContextual("async") && input.current().Span.End.Line == input.peek(1).Span.Start.Line {
			if input.peek(1).Kind == lexer.Identifier {
				return input.peek(2).Kind == lexer.Arrow
			}
			if input.peek(1).Kind == lexer.LeftParen {
				return input.arrowAfterParameters(input.index + 1)
			}
		}
	}
	return input.check(lexer.LeftParen) && input.arrowAfterParameters(input.index)
}

func (input *parser) arrowAfterParameters(start int) bool {
	depth := 0
	for index := start; index < len(input.tokens); index++ {
		switch input.tokens[index].Kind {
		case lexer.LeftParen:
			depth++
		case lexer.RightParen:
			depth--
			if depth == 0 {
				return index+1 < len(input.tokens) && input.tokens[index+1].Kind == lexer.Arrow
			}
		}
	}
	return false
}

func (input *parser) parseArrowFunction() (ast.Expression, error) {
	start := input.current().Span
	async := false
	if input.checkContextual("async") && input.peek(1).Kind != lexer.Arrow {
		input.advance()
		async = true
		previousAsync := input.async
		input.async++
		defer func() { input.async = previousAsync }()
	}
	parameters := make([]*ast.Identifier, 0)
	defaults := make([]ast.Expression, 0)
	patterns := make([]*ast.VariableDeclaration, 0)
	var rest []ast.Statement
	if input.match(lexer.LeftParen) {
		if !input.check(lexer.RightParen) {
			for {
				if input.match(lexer.Ellipsis) {
					spread := input.previous()
					parameter, pattern, err := input.parseParameterBinding("arrow rest parameter")
					if err != nil {
						return nil, err
					}
					if input.check(lexer.Assign) {
						return nil, input.errorAt(input.current(), "rest parameter cannot have a default")
					}
					rest = restParameterPrologue(parameter, pattern, len(parameters), join(spread.Span, parameter.Span()))
					if input.match(lexer.Comma) {
						return nil, input.errorAt(input.previous(), "rest parameter must be last")
					}
					break
				}
				parameter, pattern, err := input.parseParameterBinding("arrow")
				if err != nil {
					return nil, err
				}
				parameters = append(parameters, parameter)
				patterns = append(patterns, pattern)
				var defaultValue ast.Expression
				if input.match(lexer.Assign) {
					defaultValue, err = input.parseAssignment()
					if err != nil {
						return nil, err
					}
				}
				defaults = append(defaults, defaultValue)
				if !input.match(lexer.Comma) {
					break
				}
			}
		}
		if _, err := input.consume(lexer.RightParen, "expected ')' after arrow parameters"); err != nil {
			return nil, err
		}
	} else {
		parameter, err := input.consume(lexer.Identifier, "expected arrow parameter")
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, identifier(parameter))
		defaults = append(defaults, nil)
		patterns = append(patterns, nil)
	}
	if _, err := input.consume(lexer.Arrow, "expected '=>' after arrow parameters"); err != nil {
		return nil, err
	}
	if input.check(lexer.LeftBrace) {
		body, err := input.parseBlock()
		if err != nil {
			return nil, err
		}
		prologue := append(parameterPrologue(parameters, defaults, patterns), rest...)
		body.Body = append(prologue, body.Body...)
		return &ast.ArrowFunctionExpression{Base: ast.Base{Range: join(start, body.Span())}, Parameters: parameters, Body: body, Async: async}, nil
	}
	expression, err := input.parseAssignment()
	if err != nil {
		return nil, err
	}
	arrow := &ast.ArrowFunctionExpression{
		Base: ast.Base{Range: join(start, expression.Span())}, Parameters: parameters, Expression: expression, Async: async,
	}
	prologue := append(parameterPrologue(parameters, defaults, patterns), rest...)
	if len(prologue) != 0 {
		arrow.Expression = nil
		arrow.Body = &ast.BlockStatement{
			Base: ast.Base{Range: join(start, expression.Span())},
			Body: append(prologue, &ast.ReturnStatement{Base: ast.Base{Range: expression.Span()}, Argument: expression}),
		}
	}
	return arrow, nil
}

func (input *parser) parseArgumentsAfterOpen() ([]ast.Expression, lexer.Span, error) {
	arguments := make([]ast.Expression, 0)
	if !input.check(lexer.RightParen) {
		for {
			var argument ast.Expression
			var err error
			if input.match(lexer.Ellipsis) {
				spread := input.previous()
				value, valueErr := input.parseAssignment()
				if valueErr != nil {
					return nil, lexer.Span{}, valueErr
				}
				argument = &ast.SpreadElement{
					Base: ast.Base{Range: join(spread.Span, value.Span())}, Argument: value,
				}
			} else {
				argument, err = input.parseAssignment()
			}
			if err != nil {
				return nil, lexer.Span{}, err
			}
			arguments = append(arguments, argument)
			if !input.match(lexer.Comma) {
				break
			}
		}
	}
	close, err := input.consume(lexer.RightParen, "expected ')' after arguments")
	if err != nil {
		return nil, lexer.Span{}, err
	}
	return arguments, close.Span, nil
}

func (input *parser) parsePrimary() (ast.Expression, error) {
	token := input.advance()
	switch token.Kind {
	case lexer.Identifier:
		if token.Text == "async" && input.check(lexer.Function) && token.Span.End.Line == input.current().Span.Start.Line {
			input.advance()
			return input.parseFunctionExpressionAsync(token, true)
		}
		return identifier(token), nil
	case lexer.Number:
		return &ast.NumberLiteral{Base: ast.Base{Range: token.Span}, Value: token.Number}, nil
	case lexer.BigInt:
		return &ast.BigIntLiteral{Base: ast.Base{Range: token.Span}, Text: token.Text}, nil
	case lexer.String:
		return &ast.StringLiteral{Base: ast.Base{Range: token.Span}, Value: token.Text}, nil
	case lexer.TemplateHead:
		return input.parseTemplateLiteral(token)
	case lexer.True, lexer.False:
		return &ast.BoolLiteral{Base: ast.Base{Range: token.Span}, Value: token.Kind == lexer.True}, nil
	case lexer.Null:
		return &ast.NullLiteral{Base: ast.Base{Range: token.Span}}, nil
	case lexer.RegExp:
		return &ast.RegExpLiteral{Base: ast.Base{Range: token.Span}, Pattern: token.Text, Flags: token.Flags}, nil
	case lexer.This:
		return &ast.ThisExpression{Base: ast.Base{Range: token.Span}}, nil
	case lexer.Super:
		if len(input.classes) == 0 || input.classes[len(input.classes)-1].superBinding == "" {
			return nil, input.errorAt(token, "super is only valid in a derived class")
		}
		return &ast.Identifier{Base: ast.Base{Range: token.Span}, Name: input.classes[len(input.classes)-1].superBinding}, nil
	case lexer.Import:
		if input.match(lexer.Dot) {
			meta, err := input.consume(lexer.Identifier, "expected meta after import.")
			if err != nil {
				return nil, err
			}
			if meta.Text != "meta" {
				return nil, input.errorAt(meta, "expected meta after import.")
			}
			return &ast.ImportMetaExpression{Base: ast.Base{Range: join(token.Span, meta.Span)}}, nil
		}
		if input.match(lexer.LeftParen) {
			source, err := input.parseAssignment()
			if err != nil {
				return nil, err
			}
			close, err := input.consume(lexer.RightParen, "expected ')' after dynamic import specifier")
			if err != nil {
				return nil, err
			}
			return &ast.DynamicImportExpression{Base: ast.Base{Range: join(token.Span, close.Span)}, Source: source}, nil
		}
		return nil, input.errorAt(input.current(), "expected '.' or '(' after import in expression")
	case lexer.LeftParen:
		expression, err := input.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := input.consume(lexer.RightParen, "expected ')' after expression"); err != nil {
			return nil, err
		}
		return expression, nil
	case lexer.LeftBracket:
		return input.parseArrayLiteral(token)
	case lexer.LeftBrace:
		return input.parseObjectLiteral(token)
	case lexer.Function:
		return input.parseFunctionExpression(token)
	case lexer.Class:
		return input.parseClassExpression(token)
	default:
		return nil, input.errorAt(token, fmt.Sprintf("expected expression, found %s", token.Kind))
	}
}

func (input *parser) parseTemplateLiteral(head lexer.Token) (ast.Expression, error) {
	quasis := []string{head.Text}
	expressions := make([]ast.Expression, 0, 1)
	for {
		expression, err := input.parseExpression()
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, expression)
		chunk := input.current()
		if chunk.Kind != lexer.TemplateMiddle && chunk.Kind != lexer.TemplateTail {
			return nil, input.errorAt(chunk, "expected template continuation")
		}
		input.advance()
		quasis = append(quasis, chunk.Text)
		if chunk.Kind == lexer.TemplateTail {
			return &ast.TemplateLiteral{
				Base: ast.Base{Range: join(head.Span, chunk.Span)}, Quasis: quasis, Expressions: expressions,
			}, nil
		}
	}
}

func (input *parser) parseArrayLiteral(open lexer.Token) (ast.Expression, error) {
	elements := make([]ast.Expression, 0)
	for !input.check(lexer.RightBracket) {
		if input.check(lexer.EOF) {
			return nil, input.errorAt(input.current(), "expected ']' after array literal")
		}
		if input.match(lexer.Comma) {
			elements = append(elements, nil)
			continue
		}
		if input.match(lexer.Ellipsis) {
			spread := input.previous()
			argument, err := input.parseAssignment()
			if err != nil {
				return nil, err
			}
			elements = append(elements, &ast.SpreadElement{
				Base: ast.Base{Range: join(spread.Span, argument.Span())}, Argument: argument,
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBracket) {
				break
			}
			continue
		}
		element, err := input.parseAssignment()
		if err != nil {
			return nil, err
		}
		elements = append(elements, element)
		if !input.match(lexer.Comma) {
			break
		}
		if input.check(lexer.RightBracket) {
			break
		}
	}
	close, err := input.consume(lexer.RightBracket, "expected ']' after array literal")
	if err != nil {
		return nil, err
	}
	return &ast.ArrayLiteral{Base: ast.Base{Range: join(open.Span, close.Span)}, Elements: elements}, nil
}

func (input *parser) parseObjectLiteral(open lexer.Token) (ast.Expression, error) {
	properties := make([]*ast.ObjectProperty, 0)
	for !input.check(lexer.RightBrace) {
		if input.match(lexer.Ellipsis) {
			spread := input.previous()
			argument, err := input.parseAssignment()
			if err != nil {
				return nil, err
			}
			properties = append(properties, &ast.ObjectProperty{
				Base: ast.Base{Range: join(spread.Span, argument.Span())}, Value: argument, Spread: true,
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
			continue
		}
		if input.match(lexer.LeftBracket) {
			start := input.previous()
			key, err := input.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := input.consume(lexer.RightBracket, "expected ']' after computed object property"); err != nil {
				return nil, err
			}
			if _, err := input.consume(lexer.Colon, "expected ':' after computed object property"); err != nil {
				return nil, err
			}
			value, err := input.parseAssignment()
			if err != nil {
				return nil, err
			}
			properties = append(properties, &ast.ObjectProperty{
				Base: ast.Base{Range: join(start.Span, value.Span())}, KeyExpression: key, Value: value, Computed: true,
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
			continue
		}
		if input.checkContextual("async") && input.current().Span.End.Line == input.peek(1).Span.Start.Line &&
			(input.peek(1).Kind == lexer.Star ||
				(isPropertyName(input.peek(1).Kind) || input.peek(1).Kind == lexer.String || input.peek(1).Kind == lexer.Number) && input.peek(2).Kind == lexer.LeftParen) {
			start := input.advance()
			generator := input.match(lexer.Star)
			propertyName := input.advance()
			if !isPropertyName(propertyName.Kind) && propertyName.Kind != lexer.String && propertyName.Kind != lexer.Number {
				return nil, input.errorAt(propertyName, "async object method requires a property name")
			}
			propertyText := propertyName.Text
			if propertyName.Kind == lexer.Number {
				propertyText = propertyName.Lexeme
			}
			parameters, body, end, err := input.parseFunctionTailMode(true, generator)
			if err != nil {
				return nil, err
			}
			properties = append(properties, &ast.ObjectProperty{
				Base: ast.Base{Range: join(start.Span, end)}, Key: propertyText,
				Value: &ast.FunctionExpression{
					Base: ast.Base{Range: join(start.Span, end)}, Parameters: parameters, Body: body, Async: true, Generator: generator,
				},
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
			continue
		}
		if input.match(lexer.Star) {
			start := input.previous()
			propertyName := input.advance()
			if !isPropertyName(propertyName.Kind) && propertyName.Kind != lexer.String && propertyName.Kind != lexer.Number {
				return nil, input.errorAt(propertyName, "generator object method requires a property name")
			}
			propertyText := propertyName.Text
			if propertyName.Kind == lexer.Number {
				propertyText = propertyName.Lexeme
			}
			parameters, body, end, err := input.parseFunctionTailMode(false, true)
			if err != nil {
				return nil, err
			}
			properties = append(properties, &ast.ObjectProperty{
				Base: ast.Base{Range: join(start.Span, end)}, Key: propertyText,
				Value: &ast.FunctionExpression{
					Base: ast.Base{Range: join(start.Span, end)}, Parameters: parameters, Body: body, Generator: true,
				},
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
			continue
		}
		key := input.advance()
		if !isPropertyName(key.Kind) && key.Kind != lexer.String && key.Kind != lexer.Number {
			return nil, input.errorAt(key, "object property requires identifier, String, or number key")
		}
		if key.Kind == lexer.Identifier && (key.Text == "get" || key.Text == "set") &&
			(isPropertyName(input.current().Kind) || input.current().Kind == lexer.String || input.current().Kind == lexer.Number) &&
			input.peek(1).Kind == lexer.LeftParen {
			propertyName := input.advance()
			propertyText := propertyName.Text
			if propertyName.Kind == lexer.Number {
				propertyText = propertyName.Lexeme
			}
			parameters, body, end, err := input.parseFunctionTail()
			if err != nil {
				return nil, err
			}
			accessor := ast.ObjectPropertyGetter
			if key.Text == "set" {
				accessor = ast.ObjectPropertySetter
			}
			value := &ast.FunctionExpression{
				Base: ast.Base{Range: join(key.Span, end)}, Parameters: parameters, Body: body,
			}
			properties = append(properties, &ast.ObjectProperty{
				Base: ast.Base{Range: join(key.Span, end)}, Key: propertyText, Value: value, Accessor: accessor,
			})
			if !input.match(lexer.Comma) {
				break
			}
			if input.check(lexer.RightBrace) {
				break
			}
			continue
		}
		keyText := key.Text
		if key.Kind == lexer.Number {
			keyText = key.Lexeme
		}
		var value ast.Expression
		shorthand := false
		if input.match(lexer.Colon) {
			var err error
			value, err = input.parseAssignment()
			if err != nil {
				return nil, err
			}
		} else if input.check(lexer.LeftParen) {
			parameters, body, end, err := input.parseFunctionTail()
			if err != nil {
				return nil, err
			}
			value = &ast.FunctionExpression{
				Base: ast.Base{Range: join(key.Span, end)}, Parameters: parameters, Body: body,
			}
		} else if key.Kind == lexer.Identifier {
			value = identifier(key)
			shorthand = true
		} else {
			return nil, input.errorAt(input.current(), "expected ':' after object property key")
		}
		properties = append(properties, &ast.ObjectProperty{
			Base: ast.Base{Range: join(key.Span, value.Span())}, Key: keyText, Value: value, Shorthand: shorthand,
		})
		if !input.match(lexer.Comma) {
			break
		}
		if input.check(lexer.RightBrace) {
			break
		}
	}
	close, err := input.consume(lexer.RightBrace, "expected '}' after object literal")
	if err != nil {
		return nil, err
	}
	return &ast.ObjectLiteral{Base: ast.Base{Range: join(open.Span, close.Span)}, Properties: properties}, nil
}

func isPropertyName(kind lexer.Kind) bool {
	return kind == lexer.Identifier || kind >= lexer.Let && kind <= lexer.Super
}

func (input *parser) parseFunctionExpression(start lexer.Token) (ast.Expression, error) {
	return input.parseFunctionExpressionAsync(start, false)
}

func (input *parser) parseFunctionExpressionAsync(start lexer.Token, async bool) (ast.Expression, error) {
	generator := input.match(lexer.Star)
	var name *ast.Identifier
	if input.check(lexer.Identifier) {
		name = identifier(input.advance())
	}
	parameters, body, end, err := input.parseFunctionTailMode(async, generator)
	if err != nil {
		return nil, err
	}
	return &ast.FunctionExpression{
		Base: ast.Base{Range: join(start.Span, end)}, Name: name, Parameters: parameters, Body: body, Async: async, Generator: generator,
	}, nil
}

func (input *parser) finishStatement() (lexer.Span, error) {
	if input.match(lexer.Semicolon) {
		return input.previous().Span, nil
	}
	current := input.current()
	previous := input.previous()
	if current.Kind == lexer.RightBrace || current.Kind == lexer.EOF || current.Span.Start.Line > previous.Span.End.Line {
		return previous.Span, nil
	}
	return lexer.Span{}, input.errorAt(current, "expected ';' after statement")
}

func (input *parser) statementEndedAfter(token lexer.Token) bool {
	current := input.current()
	return current.Kind == lexer.Semicolon || current.Kind == lexer.RightBrace || current.Kind == lexer.EOF || current.Span.Start.Line > token.Span.End.Line
}

func (input *parser) match(kinds ...lexer.Kind) bool {
	if contains(kinds, input.current().Kind) {
		input.advance()
		return true
	}
	return false
}

func (input *parser) matchContextual(text string) bool {
	if input.current().Kind == lexer.Identifier && input.current().Text == text {
		input.advance()
		return true
	}
	return false
}

func (input *parser) checkContextual(text string) bool {
	return input.check(lexer.Identifier) && input.current().Text == text
}

func contains(kinds []lexer.Kind, kind lexer.Kind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func (input *parser) check(kind lexer.Kind) bool { return input.current().Kind == kind }

func (input *parser) peek(distance int) lexer.Token {
	index := input.index + distance
	if index < 0 || index >= len(input.tokens) {
		return input.tokens[len(input.tokens)-1]
	}
	return input.tokens[index]
}

func (input *parser) consume(kind lexer.Kind, message string) (lexer.Token, error) {
	if input.check(kind) {
		return input.advance(), nil
	}
	return lexer.Token{}, input.errorAt(input.current(), message)
}

func (input *parser) advance() lexer.Token {
	token := input.current()
	if token.Kind != lexer.EOF {
		input.index++
	}
	return token
}

func (input *parser) current() lexer.Token {
	if input.index >= len(input.tokens) {
		return input.tokens[len(input.tokens)-1]
	}
	return input.tokens[input.index]
}

func (input *parser) previous() lexer.Token {
	if input.index == 0 {
		return input.tokens[0]
	}
	return input.tokens[input.index-1]
}

func (input *parser) errorAt(token lexer.Token, message string) error {
	return &Error{Span: token.Span, Message: message}
}

func identifier(token lexer.Token) *ast.Identifier {
	return &ast.Identifier{Base: ast.Base{Range: token.Span}, Name: token.Text}
}

func join(start, end lexer.Span) lexer.Span {
	return lexer.Span{Start: start.Start, End: end.End}
}
