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
	case *ast.BoolLiteral:
		opcode := browserruntime.OpFalse
		if expression.Value {
			opcode = browserruntime.OpTrue
		}
		return compiler.emit(browserruntime.Instruction{Op: opcode}, expression.Span())
	case *ast.NullLiteral:
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNull}, expression.Span())
	case *ast.ThisExpression:
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadThis}, expression.Span())
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
	case *ast.MemberExpression:
		return compiler.compileMember(expression)
	case *ast.CallExpression:
		return compiler.compileCall(expression)
	case *ast.NewExpression:
		return compiler.compileNew(expression)
	case *ast.FunctionExpression:
		return compiler.compileFunctionExpression(expression)
	default:
		return compiler.problem(expression.Span(), fmt.Sprintf("unsupported expression %T", expression))
	}
}

func (compiler *functionCompiler) compileCall(call *ast.CallExpression) error {
	if err := compiler.compileExpression(call.Callee); err != nil {
		return err
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
	function, err := compiler.compileNestedFunction(name, expression.Parameters, expression.Body, expression.Span())
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
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewArray, A: uint32(len(array.Elements))}, array.Span()); err != nil {
		return err
	}
	for index, element := range array.Elements {
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
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpSetOwnProperty}, property.Span()); err != nil {
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
	opcode := browserruntime.OpGetOwnProperty
	if member.Computed {
		opcode = browserruntime.OpGetElement
	}
	return compiler.emit(browserruntime.Instruction{Op: opcode}, member.Span())
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
		opcode := browserruntime.OpSetOwnProperty
		if target.Computed {
			opcode = browserruntime.OpSetElement
		}
		return compiler.emit(browserruntime.Instruction{Op: opcode}, assignment.Span())
	default:
		return compiler.problem(assignment.Left.Span(), "unsupported assignment target")
	}
}

func (compiler *functionCompiler) compileUpdate(update *ast.UpdateExpression) error {
	target, ok := update.Argument.(*ast.Identifier)
	if !ok {
		return compiler.problem(update.Argument.Span(), "member updates require the later property semantic layer")
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
		opcode := browserruntime.OpDeleteOwnProperty
		if member.Computed {
			opcode = browserruntime.OpDeleteElement
		}
		return compiler.emit(browserruntime.Instruction{Op: opcode}, unary.Span())
	}
	if unary.Operator == lexer.Plus {
		return compiler.problem(unary.Span(), "unary plus requires JavaScript coercion semantics")
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
	case lexer.Less:
		return browserruntime.OpLessThan, true
	case lexer.LessEqual:
		return browserruntime.OpLessThanOrEqual, true
	case lexer.Greater:
		return browserruntime.OpGreaterThan, true
	case lexer.GreaterEqual:
		return browserruntime.OpGreaterThanOrEqual, true
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
