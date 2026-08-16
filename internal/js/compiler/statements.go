package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func (compiler *functionCompiler) compileStatement(statement ast.Statement) error {
	switch statement := statement.(type) {
	case *ast.EmptyStatement:
		return nil
	case *ast.BlockStatement:
		return compiler.compileBlock(statement)
	case *ast.ExpressionStatement:
		if err := compiler.compileExpression(statement.Expression); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, statement.Span())
	case *ast.VariableDeclaration:
		return compiler.compileVariableDeclaration(statement)
	case *ast.IfStatement:
		return compiler.compileIf(statement)
	case *ast.WhileStatement:
		return compiler.compileWhile(statement)
	case *ast.DoWhileStatement:
		return compiler.compileDoWhile(statement, "")
	case *ast.ForStatement:
		return compiler.compileFor(statement, "")
	case *ast.ForInStatement:
		return compiler.compileForIn(statement, "")
	case *ast.SwitchStatement:
		return compiler.compileSwitch(statement, "")
	case *ast.LabeledStatement:
		return compiler.compileLabeled(statement)
	case *ast.BreakStatement:
		target, found := compiler.resolveControlTarget(statement.Label, false)
		if !found {
			return compiler.problem(statement.Span(), "break has no matching target")
		}
		return compiler.emitCompletion(browserruntime.OpBreak, target.breakLabel, target.breakEnvironmentDepth, target.handlerDepth, statement.Span())
	case *ast.ContinueStatement:
		target, found := compiler.resolveControlTarget(statement.Label, true)
		if !found {
			return compiler.problem(statement.Span(), "continue has no matching loop target")
		}
		return compiler.emitCompletion(browserruntime.OpContinue, target.continueLabel, target.continueEnvironmentDepth, target.handlerDepth, statement.Span())
	case *ast.FunctionDeclaration:
		// Function declarations are instantiated in the containing Function
		// scope before any statement executes.
		return nil
	case *ast.ReturnStatement:
		if !compiler.inFunction {
			return compiler.problem(statement.Span(), "return is only valid inside a Function")
		}
		if statement.Argument == nil {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, statement.Span()); err != nil {
				return err
			}
		} else if err := compiler.compileExpression(statement.Argument); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, statement.Span())
	case *ast.ThrowStatement:
		if err := compiler.compileExpression(statement.Argument); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpThrow}, statement.Span())
	case *ast.TryStatement:
		return compiler.compileTry(statement)
	default:
		return compiler.problem(statement.Span(), fmt.Sprintf("unsupported statement %T", statement))
	}
}

func (compiler *functionCompiler) compileVariableDeclaration(declaration *ast.VariableDeclaration) error {
	for _, declarator := range declaration.Declarations {
		name, err := compiler.stringConstant(declarator.Name.Name)
		if err != nil {
			return err
		}
		if declaration.Kind == ast.VariableVar {
			if declarator.Init == nil {
				continue
			}
			if err := compiler.compileExpression(declarator.Init); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, declarator.Span()); err != nil {
				return err
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declarator.Span()); err != nil {
				return err
			}
			continue
		}
		if declaration.Kind == ast.VariableConst && declarator.Init == nil {
			return compiler.problem(declarator.Span(), "const declaration requires an initializer")
		}
		if declarator.Init == nil {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, declarator.Span()); err != nil {
				return err
			}
		} else if err := compiler.compileExpression(declarator.Init); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, declarator.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declarator.Span()); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileBlock(block *ast.BlockStatement) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, block.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	compiler.pushScope()
	if err := compiler.instantiateLexicalScope(block.Body); err != nil {
		return err
	}
	for _, child := range block.Body {
		if err := compiler.compileStatement(child); err != nil {
			return err
		}
	}
	compiler.popScope()
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, block.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return nil
}

func (compiler *functionCompiler) compileIf(statement *ast.IfStatement) error {
	if err := compiler.compileExpression(statement.Test); err != nil {
		return err
	}
	alternate := compiler.builder.NewLabel()
	end := alternate
	if statement.Alternate != nil {
		end = compiler.builder.NewLabel()
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, alternate, statement.Test.Span()); err != nil {
		return err
	}
	if err := compiler.compileStatement(statement.Consequent); err != nil {
		return err
	}
	if statement.Alternate != nil {
		if err := compiler.emitJump(browserruntime.OpJump, end, statement.Consequent.Span()); err != nil {
			return err
		}
	}
	if err := compiler.builder.Mark(alternate); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if statement.Alternate != nil {
		if err := compiler.compileStatement(statement.Alternate); err != nil {
			return err
		}
		if err := compiler.builder.Mark(end); err != nil {
			return fmt.Errorf("%w: %v", ErrCompile, err)
		}
	}
	return nil
}

func (compiler *functionCompiler) compileWhile(statement *ast.WhileStatement) error {
	return compiler.compileWhileLabeled(statement, "")
}

func (compiler *functionCompiler) compileWhileLabeled(statement *ast.WhileStatement, name string) error {
	start := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.builder.Mark(start); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if err := compiler.compileExpression(statement.Test); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, end, statement.Test.Span()); err != nil {
		return err
	}
	compiler.loops = append(compiler.loops, loopTarget{
		name: name, breakLabel: end, continueLabel: start,
		breakEnvironmentDepth: compiler.environmentDepth, continueEnvironmentDepth: compiler.environmentDepth,
		handlerDepth: compiler.handlerDepth,
	})
	err := compiler.compileStatement(statement.Body)
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJump, start, statement.Body.Span()); err != nil {
		return err
	}
	if err := compiler.builder.Mark(end); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) compileDoWhile(statement *ast.DoWhileStatement, name string) error {
	start := compiler.builder.NewLabel()
	condition := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.mark(start); err != nil {
		return err
	}
	compiler.loops = append(compiler.loops, loopTarget{
		name: name, breakLabel: end, continueLabel: condition,
		breakEnvironmentDepth: compiler.environmentDepth, continueEnvironmentDepth: compiler.environmentDepth,
		handlerDepth: compiler.handlerDepth,
	})
	err := compiler.compileStatement(statement.Body)
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err != nil {
		return err
	}
	if err := compiler.mark(condition); err != nil {
		return err
	}
	if err := compiler.compileExpression(statement.Test); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfTrue, start, statement.Test.Span()); err != nil {
		return err
	}
	return compiler.mark(end)
}

func (compiler *functionCompiler) compileFor(statement *ast.ForStatement, name string) error {
	outerDepth := compiler.environmentDepth
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	compiler.pushScope()
	defer compiler.popScope()
	if declaration := statement.InitDeclaration; declaration != nil && declaration.Kind != ast.VariableVar {
		for _, item := range declaration.Declarations {
			if err := compiler.declare(item.Name.Name, declaration.Kind == ast.VariableLet, item.Name.Span()); err != nil {
				return err
			}
			nameConstant, err := compiler.stringConstant(item.Name.Name)
			if err != nil {
				return err
			}
			mutable := uint32(0)
			if declaration.Kind == ast.VariableLet {
				mutable = 1
			}
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: nameConstant, B: mutable}, item.Name.Span()); err != nil {
				return err
			}
		}
	}
	if statement.InitDeclaration != nil {
		if err := compiler.compileVariableDeclaration(statement.InitDeclaration); err != nil {
			return err
		}
	} else if statement.InitExpression != nil {
		if err := compiler.compileExpression(statement.InitExpression); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, statement.InitExpression.Span()); err != nil {
			return err
		}
	}

	condition := compiler.builder.NewLabel()
	update := compiler.builder.NewLabel()
	cleanup := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.mark(condition); err != nil {
		return err
	}
	if statement.Test != nil {
		if err := compiler.compileExpression(statement.Test); err != nil {
			return err
		}
		if err := compiler.emitJump(browserruntime.OpJumpIfFalse, cleanup, statement.Test.Span()); err != nil {
			return err
		}
	}
	compiler.loops = append(compiler.loops, loopTarget{
		name: name, breakLabel: end, continueLabel: update,
		breakEnvironmentDepth: outerDepth, continueEnvironmentDepth: compiler.environmentDepth,
		handlerDepth: compiler.handlerDepth,
	})
	err := compiler.compileStatement(statement.Body)
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err != nil {
		return err
	}
	if err := compiler.mark(update); err != nil {
		return err
	}
	if statement.Update != nil {
		if err := compiler.compileExpression(statement.Update); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, statement.Update.Span()); err != nil {
			return err
		}
	}
	if err := compiler.emitJump(browserruntime.OpJump, condition, statement.Span()); err != nil {
		return err
	}
	if err := compiler.mark(cleanup); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return compiler.mark(end)
}

func (compiler *functionCompiler) compileForIn(statement *ast.ForInStatement, label string) error {
	outerDepth := compiler.environmentDepth
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	iterableName := compiler.temporaryName("for.iterable")
	indexName := compiler.temporaryName("for.index")
	if err := compiler.compileExpression(statement.Right); err != nil {
		return err
	}
	if !statement.Of {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpOwnKeys}, statement.Right.Span()); err != nil {
			return err
		}
	}
	if err := compiler.initializeTemporary(iterableName, statement.Span()); err != nil {
		return err
	}
	zero, err := compiler.addConstant(program.Number(0))
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: zero}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.initializeTemporary(indexName, statement.Span()); err != nil {
		return err
	}

	condition := compiler.builder.NewLabel()
	advance := compiler.builder.NewLabel()
	cleanup := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.mark(condition); err != nil {
		return err
	}
	if err := compiler.loadTemporary(indexName, statement.Span()); err != nil {
		return err
	}
	if err := compiler.loadTemporary(iterableName, statement.Span()); err != nil {
		return err
	}
	length, err := compiler.stringConstant("length")
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: length}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLessThan}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, cleanup, statement.Span()); err != nil {
		return err
	}
	if err := compiler.loadTemporary(iterableName, statement.Span()); err != nil {
		return err
	}
	if err := compiler.loadTemporary(indexName, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, statement.Span()); err != nil {
		return err
	}

	iterationScope := statement.LeftDeclaration != nil && statement.LeftDeclaration.Kind != ast.VariableVar
	if iterationScope {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, statement.Span()); err != nil {
			return err
		}
		compiler.environmentDepth++
		compiler.pushScope()
	}
	if err := compiler.initializeForBinding(statement); err != nil {
		return err
	}
	compiler.loops = append(compiler.loops, loopTarget{
		name: label, breakLabel: end, continueLabel: advance,
		breakEnvironmentDepth: outerDepth, continueEnvironmentDepth: outerDepth + 1,
		handlerDepth: compiler.handlerDepth,
	})
	err = compiler.compileStatement(statement.Body)
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err != nil {
		return err
	}
	if iterationScope {
		compiler.popScope()
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, statement.Span()); err != nil {
			return err
		}
		compiler.environmentDepth--
	}
	if err := compiler.mark(advance); err != nil {
		return err
	}
	if err := compiler.loadTemporary(indexName, statement.Span()); err != nil {
		return err
	}
	one, err := compiler.addConstant(program.Number(1))
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: one}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpAdd}, statement.Span()); err != nil {
		return err
	}
	indexConstant, err := compiler.stringConstant(indexName)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: indexConstant}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, statement.Span()); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJump, condition, statement.Span()); err != nil {
		return err
	}
	if err := compiler.mark(cleanup); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return compiler.mark(end)
}

func (compiler *functionCompiler) initializeForBinding(statement *ast.ForInStatement) error {
	if declaration := statement.LeftDeclaration; declaration != nil {
		item := declaration.Declarations[0]
		name, err := compiler.stringConstant(item.Name.Name)
		if err != nil {
			return err
		}
		if declaration.Kind == ast.VariableVar {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, item.Span()); err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, item.Span())
		}
		if err := compiler.declare(item.Name.Name, declaration.Kind == ast.VariableLet, item.Name.Span()); err != nil {
			return err
		}
		mutable := uint32(0)
		if declaration.Kind == ast.VariableLet {
			mutable = 1
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: mutable}, item.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, item.Span()); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, item.Span())
	}
	identifier, ok := statement.LeftExpression.(*ast.Identifier)
	if !ok {
		return compiler.problem(statement.LeftExpression.Span(), "for-in/of assignment currently requires an identifier")
	}
	name, err := compiler.stringConstant(identifier.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStoreBinding, A: name}, identifier.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, identifier.Span())
}

func (compiler *functionCompiler) compileSwitch(statement *ast.SwitchStatement, name string) error {
	outerDepth := compiler.environmentDepth
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	discriminant := compiler.temporaryName("switch")
	if err := compiler.compileExpression(statement.Discriminant); err != nil {
		return err
	}
	if err := compiler.initializeTemporary(discriminant, statement.Span()); err != nil {
		return err
	}
	labels := make([]browserruntime.Label, len(statement.Cases))
	defaultLabel := browserruntime.Label(0)
	for index, item := range statement.Cases {
		labels[index] = compiler.builder.NewLabel()
		if item.Test == nil {
			defaultLabel = labels[index]
			continue
		}
		if err := compiler.loadTemporary(discriminant, item.Span()); err != nil {
			return err
		}
		if err := compiler.compileExpression(item.Test); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpStrictEqual}, item.Span()); err != nil {
			return err
		}
		if err := compiler.emitJump(browserruntime.OpJumpIfTrue, labels[index], item.Span()); err != nil {
			return err
		}
	}
	cleanup := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if defaultLabel != 0 {
		if err := compiler.emitJump(browserruntime.OpJump, defaultLabel, statement.Span()); err != nil {
			return err
		}
	} else if err := compiler.emitJump(browserruntime.OpJump, cleanup, statement.Span()); err != nil {
		return err
	}
	compiler.loops = append(compiler.loops, loopTarget{
		name: name, breakLabel: end, breakEnvironmentDepth: outerDepth,
		continueEnvironmentDepth: compiler.environmentDepth, handlerDepth: compiler.handlerDepth,
	})
	for index, item := range statement.Cases {
		if err := compiler.mark(labels[index]); err != nil {
			return err
		}
		for _, child := range item.Consequent {
			if err := compiler.compileStatement(child); err != nil {
				return err
			}
		}
	}
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err := compiler.mark(cleanup); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, statement.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return compiler.mark(end)
}

func (compiler *functionCompiler) compileLabeled(statement *ast.LabeledStatement) error {
	name := statement.Label.Name
	switch body := statement.Body.(type) {
	case *ast.WhileStatement:
		return compiler.compileWhileLabeled(body, name)
	case *ast.DoWhileStatement:
		return compiler.compileDoWhile(body, name)
	case *ast.ForStatement:
		return compiler.compileFor(body, name)
	case *ast.ForInStatement:
		return compiler.compileForIn(body, name)
	case *ast.SwitchStatement:
		return compiler.compileSwitch(body, name)
	default:
		end := compiler.builder.NewLabel()
		compiler.loops = append(compiler.loops, loopTarget{
			name: name, breakLabel: end, breakEnvironmentDepth: compiler.environmentDepth,
			continueEnvironmentDepth: compiler.environmentDepth, handlerDepth: compiler.handlerDepth,
		})
		err := compiler.compileStatement(body)
		compiler.loops = compiler.loops[:len(compiler.loops)-1]
		if err != nil {
			return err
		}
		return compiler.mark(end)
	}
}

func (compiler *functionCompiler) resolveControlTarget(label *ast.Identifier, continuing bool) (loopTarget, bool) {
	for index := len(compiler.loops) - 1; index >= 0; index-- {
		target := compiler.loops[index]
		if label != nil && target.name != label.Name {
			continue
		}
		if continuing && target.continueLabel == 0 {
			if label != nil {
				return loopTarget{}, false
			}
			continue
		}
		return target, true
	}
	return loopTarget{}, false
}

func (compiler *functionCompiler) initializeTemporary(name string, span lexer.Span) error {
	constant, err := compiler.stringConstant(name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: constant, B: 1}, span); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: constant}, span); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, span)
}

func (compiler *functionCompiler) loadTemporary(name string, span lexer.Span) error {
	constant, err := compiler.stringConstant(name)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: constant}, span)
}

func (compiler *functionCompiler) compileHoistedFunction(declaration *ast.FunctionDeclaration, initialize bool) error {
	name, err := compiler.stringConstant(declaration.Name.Name)
	if err != nil {
		return err
	}
	function, err := compiler.compileNestedFunction(declaration.Name.Name, declaration.Parameters, declaration.Body, declaration.Span())
	if err != nil {
		return err
	}
	constant, err := compiler.addFunctionConstant(function)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: constant}, declaration.Span()); err != nil {
		return err
	}
	opcode := browserruntime.OpStoreBinding
	if initialize {
		opcode = browserruntime.OpInitializeBinding
	}
	if err := compiler.emit(browserruntime.Instruction{Op: opcode, A: name}, declaration.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declaration.Span())
}

func (compiler *functionCompiler) compileNestedFunction(name string, parameters []*ast.Identifier, body *ast.BlockStatement, span lexer.Span) (uint32, error) {
	index, err := compiler.owner.reserveFunction()
	if err != nil {
		return 0, err
	}
	child := newFunctionCompiler(compiler.owner, compiler, true)
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, span); err != nil {
		return 0, err
	}
	child.environmentDepth++
	for parameterIndex, parameter := range parameters {
		if err := child.declareParameter(parameter.Name, parameter.Span()); err != nil {
			return 0, err
		}
		constant, err := child.stringConstant(parameter.Name)
		if err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: constant, B: 1}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpArgument, A: uint32(parameterIndex)}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: constant}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, parameter.Span()); err != nil {
			return 0, err
		}
	}
	if err := child.instantiateFunctionScope(body.Body); err != nil {
		return 0, err
	}
	for _, statement := range body.Body {
		if err := child.compileStatement(statement); err != nil {
			return 0, err
		}
	}
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, body.Span()); err != nil {
		return 0, err
	}
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, body.Span()); err != nil {
		return 0, err
	}
	template, err := child.finish(name, uint32(len(parameters)))
	if err != nil {
		return 0, err
	}
	compiler.owner.functions[index] = template
	return index, nil
}

func (compiler *functionCompiler) compileTry(statement *ast.TryStatement) error {
	baseHandlerDepth := compiler.handlerDepth
	var finallyLabel browserruntime.Label
	if statement.Finalizer != nil {
		finallyLabel = compiler.builder.NewLabel()
		if err := compiler.emitHandler(browserruntime.HandlerFinally, finallyLabel, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth++
	}

	var catchLabel browserruntime.Label
	if statement.Handler != nil {
		catchLabel = compiler.builder.NewLabel()
		if err := compiler.emitHandler(browserruntime.HandlerCatch, catchLabel, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth++
	}
	if err := compiler.compileStatement(statement.Body); err != nil {
		return err
	}
	if statement.Handler != nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveTry}, statement.Body.Span()); err != nil {
			return err
		}
		compiler.handlerDepth--
		catchDone := compiler.builder.NewLabel()
		if err := compiler.emitJump(browserruntime.OpJump, catchDone, statement.Body.Span()); err != nil {
			return err
		}
		if err := compiler.mark(catchLabel); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterCatch}, statement.Handler.Span()); err != nil {
			return err
		}
		if err := compiler.compileCatch(statement.Handler); err != nil {
			return err
		}
		if err := compiler.mark(catchDone); err != nil {
			return err
		}
	}

	if statement.Finalizer != nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveTry}, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth--
		if err := compiler.emitJump(browserruntime.OpJump, finallyLabel, statement.Span()); err != nil {
			return err
		}
		if err := compiler.mark(finallyLabel); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterFinally}, statement.Finalizer.Span()); err != nil {
			return err
		}
		if err := compiler.compileStatement(statement.Finalizer); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEndFinally}, statement.Finalizer.Span()); err != nil {
			return err
		}
	}
	if compiler.handlerDepth != baseHandlerDepth {
		return compiler.problem(statement.Span(), "internal handler-depth imbalance")
	}
	return nil
}

func (compiler *functionCompiler) compileCatch(clause *ast.CatchClause) error {
	if clause.Parameter == nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, clause.Span()); err != nil {
			return err
		}
		return compiler.compileStatement(clause.Body)
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, clause.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	compiler.pushScope()
	defer compiler.popScope()
	if err := compiler.declare(clause.Parameter.Name, true, clause.Parameter.Span()); err != nil {
		return err
	}
	name, err := compiler.stringConstant(clause.Parameter.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: 1}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.compileStatement(clause.Body); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, clause.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return nil
}

func (compiler *functionCompiler) addFunctionConstant(index uint32) (uint32, error) {
	return compiler.addConstant(program.Function(index))
}
