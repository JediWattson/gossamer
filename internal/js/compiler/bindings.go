package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func (compiler *functionCompiler) compileAssignmentPattern(pattern ast.Expression) error {
	switch pattern := pattern.(type) {
	case *ast.Identifier:
		binding, exists := compiler.resolve(pattern.Name)
		if !exists {
			return compiler.problem(pattern.Span(), fmt.Sprintf("unknown binding %q", pattern.Name))
		}
		if !binding.mutable {
			return compiler.problem(pattern.Span(), fmt.Sprintf("cannot assign to const binding %q", pattern.Name))
		}
		name, err := compiler.stringConstant(pattern.Name)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, pattern.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, pattern.Span())
	case *ast.MemberExpression:
		value := compiler.temporaryName("assignment.pattern.value")
		if err := compiler.initializeTemporary(value, pattern.Span()); err != nil {
			return err
		}
		if err := compiler.compileExpression(pattern.Object); err != nil {
			return err
		}
		if err := compiler.compileMemberKey(pattern); err != nil {
			return err
		}
		if err := compiler.loadTemporary(value, pattern.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpSetProperty}, pattern.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, pattern.Span())
	case *ast.ArrayLiteral:
		return compiler.compileArrayAssignmentPattern(pattern)
	case *ast.ObjectLiteral:
		return compiler.compileObjectAssignmentPattern(pattern)
	default:
		return compiler.problem(pattern.Span(), "unsupported assignment pattern")
	}
}

func (compiler *functionCompiler) compileArrayAssignmentPattern(pattern *ast.ArrayLiteral) error {
	temporary := compiler.temporaryName("array.assignment")
	if err := compiler.initializeTemporary(temporary, pattern.Span()); err != nil {
		return err
	}
	for index, raw := range pattern.Elements {
		if raw == nil {
			continue
		}
		target := raw
		rest := false
		if spread, ok := raw.(*ast.SpreadElement); ok {
			target = spread.Argument
			rest = true
		}
		var defaultValue ast.Expression
		if assignment, ok := target.(*ast.AssignmentExpression); ok && assignment.Operator == lexer.Assign {
			target = assignment.Left
			defaultValue = assignment.Right
		}
		if err := compiler.loadTemporary(temporary, raw.Span()); err != nil {
			return err
		}
		if rest {
			if err := compiler.compileArraySliceFromStack(index, raw.Span()); err != nil {
				return err
			}
		} else {
			property, err := compiler.addConstant(program.Number(float64(index)))
			if err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: property}, raw.Span()); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, raw.Span()); err != nil {
				return err
			}
			if defaultValue != nil {
				if err := compiler.compileBindingDefault(defaultValue, raw.Span()); err != nil {
					return err
				}
			}
		}
		if err := compiler.compileAssignmentPattern(target); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileObjectAssignmentPattern(pattern *ast.ObjectLiteral) error {
	temporary := compiler.temporaryName("object.assignment")
	if err := compiler.initializeTemporary(temporary, pattern.Span()); err != nil {
		return err
	}
	for _, property := range pattern.Properties {
		if property.Spread {
			if err := compiler.compileObjectAssignmentRest(pattern, property, temporary); err != nil {
				return err
			}
			continue
		}
		target := property.Value
		var defaultValue ast.Expression
		if assignment, ok := target.(*ast.AssignmentExpression); ok && assignment.Operator == lexer.Assign {
			target = assignment.Left
			defaultValue = assignment.Right
		}
		if err := compiler.loadTemporary(temporary, property.Span()); err != nil {
			return err
		}
		if property.Computed {
			if err := compiler.compileExpression(property.KeyExpression); err != nil {
				return err
			}
		} else {
			key, err := compiler.stringConstant(property.Key)
			if err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, property.Span()); err != nil {
				return err
			}
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, property.Span()); err != nil {
			return err
		}
		if defaultValue != nil {
			if err := compiler.compileBindingDefault(defaultValue, property.Span()); err != nil {
				return err
			}
		}
		if err := compiler.compileAssignmentPattern(target); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileObjectAssignmentRest(pattern *ast.ObjectLiteral, rest *ast.ObjectProperty, source string) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewObject}, rest.Span()); err != nil {
		return err
	}
	if err := compiler.loadTemporary(source, rest.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCopyDataProperties}, rest.Span()); err != nil {
		return err
	}
	temporary := compiler.temporaryName("object.assignment.rest")
	if err := compiler.initializeTemporary(temporary, rest.Span()); err != nil {
		return err
	}
	for _, property := range pattern.Properties {
		if property.Spread {
			continue
		}
		if err := compiler.loadTemporary(temporary, property.Span()); err != nil {
			return err
		}
		if property.Computed {
			if err := compiler.compileExpression(property.KeyExpression); err != nil {
				return err
			}
		} else {
			key, err := compiler.stringConstant(property.Key)
			if err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, property.Span()); err != nil {
				return err
			}
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeleteProperty}, property.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, property.Span()); err != nil {
			return err
		}
	}
	if err := compiler.loadTemporary(temporary, rest.Span()); err != nil {
		return err
	}
	return compiler.compileAssignmentPattern(rest.Value)
}

// compileBindingPattern consumes the value at the top of the stack and binds
// every leaf in a recursively nested binding pattern.
func (compiler *functionCompiler) compileBindingPattern(kind ast.VariableKind, pattern *ast.BindingPattern) error {
	if pattern.Name != nil {
		name, err := compiler.stringConstant(pattern.Name.Name)
		if err != nil {
			return err
		}
		opcode := browserruntime.OpInitializeBinding
		if kind == ast.VariableVar {
			opcode = browserruntime.OpStoreBinding
		}
		if err := compiler.emit(browserruntime.Instruction{Op: opcode, A: name}, pattern.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, pattern.Span())
	}
	if pattern.Array != nil {
		return compiler.compileRecursiveArrayBinding(kind, pattern)
	}
	return compiler.compileRecursiveObjectBinding(kind, pattern)
}

func (compiler *functionCompiler) compileRecursiveArrayBinding(kind ast.VariableKind, pattern *ast.BindingPattern) error {
	temporary := compiler.temporaryName("array.binding")
	if err := compiler.initializeTemporary(temporary, pattern.Span()); err != nil {
		return err
	}
	for index, element := range pattern.Array {
		if element == nil {
			continue
		}
		if err := compiler.loadTemporary(temporary, element.Span()); err != nil {
			return err
		}
		if element.Rest {
			if err := compiler.compileArraySliceFromStack(index, element.Span()); err != nil {
				return err
			}
		} else {
			property, err := compiler.addConstant(program.Number(float64(index)))
			if err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: property}, element.Span()); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, element.Span()); err != nil {
				return err
			}
			if element.Default != nil {
				if err := compiler.compileBindingDefault(element.Default, element.Span()); err != nil {
					return err
				}
			}
		}
		if err := compiler.compileBindingPattern(kind, element.Pattern); err != nil {
			return err
		}
	}
	return nil
}

// compileArraySliceFromStack replaces the array-like value on top of the
// stack with Array.prototype.slice.call(value, start). Calling the intrinsic
// explicitly also handles array-like argument objects used by rest lowering.
func (compiler *functionCompiler) compileArraySliceFromStack(startIndex int, span lexer.Span) error {
	value := compiler.temporaryName("array.slice.source")
	if err := compiler.initializeTemporary(value, span); err != nil {
		return err
	}
	array, err := compiler.stringConstant("Array")
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: array}, span); err != nil {
		return err
	}
	for _, property := range []string{"prototype", "slice", "call"} {
		key, err := compiler.stringConstant(property)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, span); err != nil {
			return err
		}
		if property != "call" {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, span); err != nil {
				return err
			}
		}
	}
	if err := compiler.loadTemporary(value, span); err != nil {
		return err
	}
	start, err := compiler.addConstant(program.Number(float64(startIndex)))
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: start}, span); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethod, A: 2}, span)
}

func (compiler *functionCompiler) compileRecursiveObjectBinding(kind ast.VariableKind, pattern *ast.BindingPattern) error {
	temporary := compiler.temporaryName("object.binding")
	if err := compiler.initializeTemporary(temporary, pattern.Span()); err != nil {
		return err
	}
	for _, element := range pattern.Object {
		if element == nil {
			continue
		}
		if element.Rest {
			if err := compiler.compileObjectRestBinding(kind, pattern, element, temporary); err != nil {
				return err
			}
			continue
		}
		if err := compiler.loadTemporary(temporary, element.Span()); err != nil {
			return err
		}
		key, err := compiler.stringConstant(element.Key)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, element.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, element.Span()); err != nil {
			return err
		}
		if element.Default != nil {
			if err := compiler.compileBindingDefault(element.Default, element.Span()); err != nil {
				return err
			}
		}
		if err := compiler.compileBindingPattern(kind, element.Pattern); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileObjectRestBinding(kind ast.VariableKind, pattern *ast.BindingPattern, rest *ast.ObjectBindingElement, source string) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpNewObject}, rest.Span()); err != nil {
		return err
	}
	if err := compiler.loadTemporary(source, rest.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCopyDataProperties}, rest.Span()); err != nil {
		return err
	}
	restTemporary := compiler.temporaryName("object.rest")
	if err := compiler.initializeTemporary(restTemporary, rest.Span()); err != nil {
		return err
	}
	for _, element := range pattern.Object {
		if element == nil || element.Rest {
			continue
		}
		if err := compiler.loadTemporary(restTemporary, element.Span()); err != nil {
			return err
		}
		key, err := compiler.stringConstant(element.Key)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, element.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeleteProperty}, element.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, element.Span()); err != nil {
			return err
		}
	}
	if err := compiler.loadTemporary(restTemporary, rest.Span()); err != nil {
		return err
	}
	return compiler.compileBindingPattern(kind, rest.Pattern)
}
