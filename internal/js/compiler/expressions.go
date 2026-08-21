package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func (compiler *functionCompiler) compileExpression(expression ast.Expression) error {
	switch expression := expression.(type) {
	case *ast.Identifier:
		if expression.Name == "arguments" && compiler.inFunction {
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpArguments}, expression.Span())
		}
		if _, exists := compiler.resolve(expression.Name); !exists {
			return compiler.problem(expression.Span(), fmt.Sprintf("unknown binding %q", expression.Name))
		}
		name, err := compiler.stringConstant(expression.Name)
		if err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: name}, expression.Span())
	case *ast.NumberLiteral:
		index, err := compiler.addConstant(program.Number(expression.Value))
		if err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: index}, expression.Span())
	case *ast.StringLiteral:
		index, err := compiler.stringConstant(expression.Value)
		if err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: index}, expression.Span())
	case *ast.TemplateLiteral:
		return compiler.compileTemplateLiteral(expression)
	case *ast.BoolLiteral:
		opcode := browserruntime.OpFalse
		if expression.Value {
			opcode = browserruntime.OpTrue
		}
		return compiler.emit(browserruntime.Instruction{Op: opcode}, expression.Span())
	case *ast.NullLiteral:
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNull}, expression.Span())
	case *ast.RegExpLiteral:
		index, err := compiler.addConstant(program.RegExp(expression.Pattern, expression.Flags))
		if err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: index}, expression.Span())
	case *ast.ThisExpression:
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadThis}, expression.Span())
	case *ast.ImportMetaExpression:
		return compiler.compileImportMeta(expression)
	case *ast.DynamicImportExpression:
		return compiler.compileDynamicImport(expression)
	case *ast.AwaitExpression:
		return compiler.problem(expression.Span(), "await must be lowered inside an async Function")
	case *ast.YieldExpression:
		return compiler.problem(expression.Span(), "yield must be lowered inside a generator Function")
	case *ast.ArrayLiteral:
		return compiler.compileArray(expression)
	case *ast.ObjectLiteral:
		return compiler.compileObject(expression)
	case *ast.UnaryExpression:
		return compiler.compileUnary(expression)
	case *ast.UpdateExpression:
		return compiler.compileUpdate(expression)
	case *ast.BinaryExpression:
		return compiler.compileBinary(expression)
	case *ast.ConditionalExpression:
		return compiler.compileConditional(expression)
	case *ast.AssignmentExpression:
		return compiler.compileAssignment(expression)
	case *ast.SequenceExpression:
		for index, item := range expression.Expressions {
			if err := compiler.compileExpression(item); err != nil {
				return err
			}
			if index+1 != len(expression.Expressions) {
				if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, item.Span()); err != nil {
					return err
				}
			}
		}
		return nil
	case *ast.MemberExpression:
		return compiler.compileMember(expression)
	case *ast.CallExpression:
		return compiler.compileCall(expression)
	case *ast.NewExpression:
		return compiler.compileNew(expression)
	case *ast.FunctionExpression:
		return compiler.compileFunctionExpression(expression)
	case *ast.ArrowFunctionExpression:
		return compiler.compileArrowFunctionExpression(expression)
	default:
		return compiler.problem(expression.Span(), fmt.Sprintf("unsupported expression %T", expression))
	}
}

func (compiler *functionCompiler) compileDynamicImport(expression *ast.DynamicImportExpression) error {
	root := compiler
	for root.parent != nil {
		root = root.parent
	}
	if !root.moduleRoot {
		return compiler.problem(expression.Span(), "dynamic import is currently only supported in modules")
	}
	root.moduleUsesImport = true
	bindingName, err := compiler.stringConstant(dynamicImportBinding)
	if err != nil {
		return err
	}
	methodName, err := compiler.stringConstant("import")
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: bindingName}, expression.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: methodName}, expression.Span()); err != nil {
		return err
	}
	if err := compiler.compileExpression(expression.Source); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethod, A: 1}, expression.Span())
}

func (compiler *functionCompiler) compileImportMeta(expression *ast.ImportMetaExpression) error {
	root := compiler
	for root.parent != nil {
		root = root.parent
	}
	if !root.moduleRoot {
		return compiler.problem(expression.Span(), "import.meta is only valid in a module")
	}
	root.moduleUsesImportMeta = true
	name, err := compiler.stringConstant(importMetaBinding)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: name}, expression.Span())
}

func (compiler *functionCompiler) compileTemplateLiteral(expression *ast.TemplateLiteral) error {
	if len(expression.Quasis) != len(expression.Expressions)+1 {
		return compiler.problem(expression.Span(), "malformed template literal")
	}
	first, err := compiler.stringConstant(expression.Quasis[0])
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: first}, expression.Span()); err != nil {
		return err
	}
	for index, item := range expression.Expressions {
		if err := compiler.compileExpression(item); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpAdd}, item.Span()); err != nil {
			return err
		}
		quasi, err := compiler.stringConstant(expression.Quasis[index+1])
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: quasi}, expression.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpAdd}, expression.Span()); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileArrowFunctionExpression(expression *ast.ArrowFunctionExpression) error {
	body := expression.Body
	if body == nil {
		body = &ast.BlockStatement{
			Base: ast.Base{Range: expression.Expression.Span()},
			Body: []ast.Statement{&ast.ReturnStatement{Base: ast.Base{Range: expression.Expression.Span()}, Argument: expression.Expression}},
		}
	}
	function, err := compiler.compileNestedFunction("", expression.Parameters, body, expression.Span(), expression.Async, false)
	if err != nil {
		return err
	}
	constant, err := compiler.addFunctionConstant(function)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: constant}, expression.Span())
}

func (compiler *functionCompiler) compileCall(call *ast.CallExpression) error {
	spread := hasSpread(call.Arguments)
	if member, ok := call.Callee.(*ast.MemberExpression); ok {
		if err := compiler.compileExpression(member.Object); err != nil {
			return err
		}
		if err := compiler.compileMemberKey(member); err != nil {
			return err
		}
		if spread {
			if err := compiler.compileArray(&ast.ArrayLiteral{Base: call.Base, Elements: call.Arguments}); err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethodSpread}, call.Span())
		}
		for _, argument := range call.Arguments {
			if err := compiler.compileExpression(argument); err != nil {
				return err
			}
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethod, A: uint32(len(call.Arguments))}, call.Span())
	}
	if err := compiler.compileExpression(call.Callee); err != nil {
		return err
	}
	if spread {
		if err := compiler.compileArray(&ast.ArrayLiteral{Base: call.Base, Elements: call.Arguments}); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallSpread}, call.Span())
	}
	for _, argument := range call.Arguments {
		if err := compiler.compileExpression(argument); err != nil {
			return err
		}
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCall, A: uint32(len(call.Arguments))}, call.Span())
}

func (compiler *functionCompiler) compileNew(expression *ast.NewExpression) error {
	if err := compiler.compileExpression(expression.Callee); err != nil {
		return err
	}
	if hasSpread(expression.Arguments) {
		if err := compiler.compileArray(&ast.ArrayLiteral{Base: expression.Base, Elements: expression.Arguments}); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstructSpread}, expression.Span())
	}
	for _, argument := range expression.Arguments {
		if err := compiler.compileExpression(argument); err != nil {
			return err
		}
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstruct, A: uint32(len(expression.Arguments))}, expression.Span())
}

func (compiler *functionCompiler) compileFunctionExpression(expression *ast.FunctionExpression) error {
	name := ""
	if expression.Name != nil {
		name = expression.Name.Name
	}
	function, err := compiler.compileNestedFunction(name, expression.Parameters, expression.Body, expression.Span(), expression.Async, expression.Generator)
	if err != nil {
		return err
	}
	constant, err := compiler.addFunctionConstant(function)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: constant}, expression.Span())
}

func (compiler *functionCompiler) compileArray(array *ast.ArrayLiteral) error {
	if !hasSpread(array.Elements) {
		return compiler.compileFixedArray(array.Elements, array.Span())
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewArray}, array.Span()); err != nil {
		return err
	}
	for _, element := range array.Elements {
		if spread, ok := element.(*ast.SpreadElement); ok {
			if err := compiler.compileExpression(spread.Argument); err != nil {
				return err
			}
		} else if err := compiler.compileFixedArray([]ast.Expression{element}, array.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpAppendSpread}, array.Span()); err != nil {
			return err
		}
	}
	return nil
}

func hasSpread(expressions []ast.Expression) bool {
	for _, expression := range expressions {
		if _, ok := expression.(*ast.SpreadElement); ok {
			return true
		}
	}
	return false
}

func (compiler *functionCompiler) compileFixedArray(elements []ast.Expression, span lexer.Span) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewArray, A: uint32(len(elements))}, span); err != nil {
		return err
	}
	for index, element := range elements {
		if element == nil {
			continue
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDup}, element.Span()); err != nil {
			return err
		}
		constant, err := compiler.addConstant(program.Number(float64(index)))
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: constant}, element.Span()); err != nil {
			return err
		}
		if err := compiler.compileExpression(element); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpSetElement}, element.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, element.Span()); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileObject(object *ast.ObjectLiteral) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewObject}, object.Span()); err != nil {
		return err
	}
	for _, property := range object.Properties {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDup}, property.Span()); err != nil {
			return err
		}
		name, err := compiler.stringConstant(property.Key)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: name}, property.Span()); err != nil {
			return err
		}
		if err := compiler.compileExpression(property.Value); err != nil {
			return err
		}
		opcode := browserruntime.OpSetOwnProperty
		mode := uint32(0)
		if property.Accessor != ast.ObjectPropertyData {
			opcode = browserruntime.OpDefineAccessor
			if property.Accessor == ast.ObjectPropertySetter {
				mode = 1
			}
		}
		if err := compiler.emit(browserruntime.Instruction{Op: opcode, A: mode}, property.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, property.Span()); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileMember(member *ast.MemberExpression) error {
	if err := compiler.compileExpression(member.Object); err != nil {
		return err
	}
	if err := compiler.compileMemberKey(member); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, member.Span())
}

func (compiler *functionCompiler) compileMemberKey(member *ast.MemberExpression) error {
	if member.Computed {
		return compiler.compileExpression(member.Property)
	}
	property, ok := member.Property.(*ast.Identifier)
	if !ok {
		return compiler.problem(member.Property.Span(), "named property is not an identifier")
	}
	name, err := compiler.stringConstant(property.Name)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: name}, property.Span())
}

func (compiler *functionCompiler) compileAssignment(assignment *ast.AssignmentExpression) error {
	operator := assignment.Operator
	if operator == 0 {
		operator = lexer.Assign
	}
	if operator != lexer.Assign {
		binary, ok := assignmentBinaryOperator(operator)
		if !ok {
			return compiler.problem(assignment.Span(), fmt.Sprintf("unsupported assignment operator %s", operator))
		}
		opcode, ok := binaryOpcode(binary)
		if !ok {
			return compiler.problem(assignment.Span(), fmt.Sprintf("unsupported compound operator %s", operator))
		}
		switch target := assignment.Left.(type) {
		case *ast.Identifier:
			binding, exists := compiler.resolve(target.Name)
			if !exists {
				return compiler.problem(target.Span(), fmt.Sprintf("unknown binding %q", target.Name))
			}
			if !binding.mutable {
				return compiler.problem(target.Span(), fmt.Sprintf("cannot assign to const binding %q", target.Name))
			}
			name, err := compiler.stringConstant(target.Name)
			if err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: name}, target.Span()); err != nil {
				return err
			}
			if err := compiler.compileExpression(assignment.Right); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: opcode}, assignment.Span()); err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, assignment.Span())
		case *ast.MemberExpression:
			if err := compiler.compileExpression(target.Object); err != nil {
				return err
			}
			if err := compiler.compileMemberKey(target); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDupPair}, target.Span()); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, target.Span()); err != nil {
				return err
			}
			if err := compiler.compileExpression(assignment.Right); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: opcode}, assignment.Span()); err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpSetProperty}, assignment.Span())
		default:
			return compiler.problem(assignment.Left.Span(), "unsupported compound assignment target")
		}
	}
	switch target := assignment.Left.(type) {
	case *ast.Identifier:
		binding, exists := compiler.resolve(target.Name)
		if !exists {
			return compiler.problem(target.Span(), fmt.Sprintf("unknown binding %q", target.Name))
		}
		if !binding.mutable {
			return compiler.problem(target.Span(), fmt.Sprintf("cannot assign to const binding %q", target.Name))
		}
		if err := compiler.compileExpression(assignment.Right); err != nil {
			return err
		}
		name, err := compiler.stringConstant(target.Name)
		if err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, assignment.Span())
	case *ast.MemberExpression:
		if err := compiler.compileExpression(target.Object); err != nil {
			return err
		}
		if err := compiler.compileMemberKey(target); err != nil {
			return err
		}
		if err := compiler.compileExpression(assignment.Right); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpSetProperty}, assignment.Span())
	default:
		return compiler.problem(assignment.Left.Span(), "unsupported assignment target")
	}
}

func assignmentBinaryOperator(operator lexer.Kind) (lexer.Kind, bool) {
	switch operator {
	case lexer.PlusAssign:
		return lexer.Plus, true
	case lexer.MinusAssign:
		return lexer.Minus, true
	case lexer.StarAssign:
		return lexer.Star, true
	case lexer.SlashAssign:
		return lexer.Slash, true
	case lexer.PercentAssign:
		return lexer.Percent, true
	case lexer.AmpersandAssign:
		return lexer.Ampersand, true
	case lexer.PipeAssign:
		return lexer.Pipe, true
	case lexer.CaretAssign:
		return lexer.Caret, true
	case lexer.ShiftLeftAssign:
		return lexer.ShiftLeft, true
	case lexer.ShiftRightAssign:
		return lexer.ShiftRight, true
	case lexer.UnsignedShiftRightAssign:
		return lexer.UnsignedShiftRight, true
	default:
		return 0, false
	}
}

func (compiler *functionCompiler) compileUpdate(update *ast.UpdateExpression) error {
	if target, ok := update.Argument.(*ast.MemberExpression); ok {
		if err := compiler.compileExpression(target.Object); err != nil {
			return err
		}
		if err := compiler.compileMemberKey(target); err != nil {
			return err
		}
		decrement := uint32(0)
		if update.Operator == lexer.MinusMinus {
			decrement = 1
		}
		prefix := uint32(0)
		if update.Prefix {
			prefix = 1
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUpdateProperty, A: decrement, B: prefix}, update.Span())
	}
	target, ok := update.Argument.(*ast.Identifier)
	if !ok {
		return compiler.problem(update.Argument.Span(), "unsupported update target")
	}
	binding, exists := compiler.resolve(target.Name)
	if !exists {
		return compiler.problem(target.Span(), fmt.Sprintf("unknown binding %q", target.Name))
	}
	if !binding.mutable {
		return compiler.problem(target.Span(), fmt.Sprintf("cannot update const binding %q", target.Name))
	}
	name, err := compiler.stringConstant(target.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: name}, update.Span()); err != nil {
		return err
	}
	if !update.Prefix {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDup}, update.Span()); err != nil {
			return err
		}
	}
	opcode := browserruntime.OpIncrement
	if update.Operator == lexer.MinusMinus {
		opcode = browserruntime.OpDecrement
	}
	if err := compiler.emit(browserruntime.Instruction{Op: opcode}, update.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, update.Span()); err != nil {
		return err
	}
	if !update.Prefix {
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, update.Span())
	}
	return nil
}

func (compiler *functionCompiler) compileUnary(unary *ast.UnaryExpression) error {
	if unary.Operator == lexer.Delete {
		member, ok := unary.Argument.(*ast.MemberExpression)
		if !ok {
			return compiler.problem(unary.Span(), "delete currently requires a member expression")
		}
		if err := compiler.compileExpression(member.Object); err != nil {
			return err
		}
		if err := compiler.compileMemberKey(member); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeleteProperty}, unary.Span())
	}
	if unary.Operator == lexer.Plus {
		if err := compiler.compileExpression(unary.Argument); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpToNumber}, unary.Span())
	}
	if unary.Operator == lexer.Void {
		if err := compiler.compileExpression(unary.Argument); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, unary.Argument.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, unary.Span())
	}
	if unary.Operator == lexer.Tilde {
		if err := compiler.compileExpression(unary.Argument); err != nil {
			return err
		}
		minusOne, err := compiler.addConstant(program.Number(-1))
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: minusOne}, unary.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpBitwiseXor}, unary.Span())
	}
	if unary.Operator == lexer.Typeof {
		if identifier, ok := unary.Argument.(*ast.Identifier); ok {
			name, err := compiler.stringConstant(identifier.Name)
			if err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpTypeOfBinding, A: name}, unary.Span())
		}
	}
	if err := compiler.compileExpression(unary.Argument); err != nil {
		return err
	}
	opcode := browserruntime.OpLogicalNot
	switch unary.Operator {
	case lexer.Minus:
		opcode = browserruntime.OpNegate
	case lexer.Typeof:
		opcode = browserruntime.OpTypeOf
	case lexer.Bang:
	default:
		return compiler.problem(unary.Span(), fmt.Sprintf("unsupported unary operator %s", unary.Operator))
	}
	return compiler.emit(browserruntime.Instruction{Op: opcode}, unary.Span())
}

func (compiler *functionCompiler) compileBinary(binary *ast.BinaryExpression) error {
	if binary.Operator == lexer.AndAnd || binary.Operator == lexer.OrOr || binary.Operator == lexer.Nullish {
		return compiler.compileLogical(binary)
	}
	opcode, ok := binaryOpcode(binary.Operator)
	if !ok {
		return compiler.problem(binary.Span(), fmt.Sprintf("operator %s requires later ECMAScript semantics", binary.Operator))
	}
	if err := compiler.compileExpression(binary.Left); err != nil {
		return err
	}
	if err := compiler.compileExpression(binary.Right); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: opcode}, binary.Span())
}

func binaryOpcode(operator lexer.Kind) (browserruntime.Opcode, bool) {
	switch operator {
	case lexer.Plus:
		return browserruntime.OpAdd, true
	case lexer.Minus:
		return browserruntime.OpSubtract, true
	case lexer.Star:
		return browserruntime.OpMultiply, true
	case lexer.Slash:
		return browserruntime.OpDivide, true
	case lexer.Percent:
		return browserruntime.OpRemainder, true
	case lexer.Ampersand:
		return browserruntime.OpBitwiseAnd, true
	case lexer.Pipe:
		return browserruntime.OpBitwiseOr, true
	case lexer.Caret:
		return browserruntime.OpBitwiseXor, true
	case lexer.ShiftLeft:
		return browserruntime.OpShiftLeft, true
	case lexer.ShiftRight:
		return browserruntime.OpShiftRight, true
	case lexer.UnsignedShiftRight:
		return browserruntime.OpUnsignedShiftRight, true
	case lexer.StrictEqual:
		return browserruntime.OpStrictEqual, true
	case lexer.StrictNotEqual:
		return browserruntime.OpStrictNotEqual, true
	case lexer.EqualEqual:
		return browserruntime.OpEqual, true
	case lexer.BangEqual:
		return browserruntime.OpNotEqual, true
	case lexer.Less:
		return browserruntime.OpLessThan, true
	case lexer.LessEqual:
		return browserruntime.OpLessThanOrEqual, true
	case lexer.Greater:
		return browserruntime.OpGreaterThan, true
	case lexer.GreaterEqual:
		return browserruntime.OpGreaterThanOrEqual, true
	case lexer.In:
		return browserruntime.OpIn, true
	case lexer.Instanceof:
		return browserruntime.OpInstanceOf, true
	default:
		return 0, false
	}
}

func (compiler *functionCompiler) compileLogical(binary *ast.BinaryExpression) error {
	if err := compiler.compileExpression(binary.Left); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDup}, binary.Left.Span()); err != nil {
		return err
	}
	end := compiler.builder.NewLabel()
	if binary.Operator == lexer.Nullish {
		right := compiler.builder.NewLabel()
		if err := compiler.emitJump(browserruntime.OpJumpIfNullish, right, binary.Left.Span()); err != nil {
			return err
		}
		if err := compiler.emitJump(browserruntime.OpJump, end, binary.Left.Span()); err != nil {
			return err
		}
		if err := compiler.builder.Mark(right); err != nil {
			return fmt.Errorf("%w: %v", ErrCompile, err)
		}
	} else {
		opcode := browserruntime.OpJumpIfFalse
		if binary.Operator == lexer.OrOr {
			opcode = browserruntime.OpJumpIfTrue
		}
		if err := compiler.emitJump(opcode, end, binary.Left.Span()); err != nil {
			return err
		}
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, binary.Left.Span()); err != nil {
		return err
	}
	if err := compiler.compileExpression(binary.Right); err != nil {
		return err
	}
	if err := compiler.builder.Mark(end); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) compileConditional(expression *ast.ConditionalExpression) error {
	if err := compiler.compileExpression(expression.Test); err != nil {
		return err
	}
	alternate := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, alternate, expression.Test.Span()); err != nil {
		return err
	}
	if err := compiler.compileExpression(expression.Consequent); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJump, end, expression.Consequent.Span()); err != nil {
		return err
	}
	if err := compiler.builder.Mark(alternate); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if err := compiler.compileExpression(expression.Alternate); err != nil {
		return err
	}
	if err := compiler.builder.Mark(end); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}
