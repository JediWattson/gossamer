package runtime_test

import (
	"bytes"
	"context"
	"testing"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func FuzzBytecodeRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add(browserruntime.Assemble(
		browserruntime.Instruction{Op: browserruntime.OpConstant, A: 1, B: 2},
		browserruntime.Instruction{Op: browserruntime.OpReturn},
	))
	f.Fuzz(func(t *testing.T, code []byte) {
		if len(code) > 1024 {
			code = code[:1024]
		}
		instructions, err := browserruntime.DecodeBytecode(code)
		if err != nil {
			return
		}
		if encoded := browserruntime.Assemble(instructions...); !bytes.Equal(encoded, code) {
			t.Fatalf("round trip changed %x to %x", code, encoded)
		}
	})
}

func FuzzInterpreterPreservesStoreInvariants(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 7, 1, 0, 8, 0, 0})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 96 {
			operations = operations[:96]
		}
		count := (len(operations) + 2) / 3
		if count == 0 {
			count = 1
		}
		instructions := make([]browserruntime.Instruction, 0, count+1)
		for index := 0; index < count; index++ {
			var raw [3]byte
			for offset := range raw {
				position := index*3 + offset
				if position < len(operations) {
					raw[offset] = operations[position]
				}
			}
			opcode := browserruntime.Opcode(raw[0]%byte(browserruntime.OpNotEqual) + 1)
			instruction := browserruntime.Instruction{Op: opcode, A: uint32(raw[1]), B: uint32(raw[2])}
			switch opcode {
			case browserruntime.OpConstant, browserruntime.OpLoadBinding, browserruntime.OpTypeOfBinding, browserruntime.OpDeclareBinding, browserruntime.OpInitializeBinding, browserruntime.OpStoreBinding, browserruntime.OpCreateClosure:
				instruction.A %= 4
				if opcode == browserruntime.OpDeclareBinding {
					instruction.B &= 1
				}
			case browserruntime.OpJump, browserruntime.OpJumpIfTrue, browserruntime.OpJumpIfFalse, browserruntime.OpJumpIfNullish, browserruntime.OpBreak, browserruntime.OpContinue:
				instruction.A %= uint32(count + 1)
				if opcode == browserruntime.OpBreak || opcode == browserruntime.OpContinue {
					instruction.B = 0
				}
			case browserruntime.OpEnterTry:
				instruction.A %= uint32(count + 1)
				instruction.B = uint32(browserruntime.HandlerCatch)
				if raw[2]&1 != 0 {
					instruction.B = uint32(browserruntime.HandlerFinally)
				}
			case browserruntime.OpCall, browserruntime.OpCallNative, browserruntime.OpConstruct, browserruntime.OpCallMethod:
				instruction.A %= 4
			case browserruntime.OpNewArray:
				instruction.A %= 8
			case browserruntime.OpUpdateProperty:
				instruction.A &= 1
				instruction.B &= 1
			}
			instructions = append(instructions, instruction)
		}
		instructions = append(instructions, browserruntime.Instruction{Op: browserruntime.OpReturn})

		realm, err := browserruntime.NewRealm(900, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer realm.Close()
		interpreter := browserruntime.NewInterpreter(browserruntime.InterpreterConfig{MaxInstructions: 64, MaxCallDepth: 8})
		_, err = realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
			function, err := task.NewBytecodeFunction(
				memory.NullValue(), memory.NullValue(), 0,
				browserruntime.Assemble(instructions...),
				[]memory.Value{memory.UndefinedValue(), memory.NullValue(), memory.BoolValue(true), memory.NumberValue(1)},
			)
			if err != nil {
				return err
			}
			_, _ = interpreter.Execute(task, function)
			return task.Realm.Store().CheckInvariants()
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := realm.Store().CheckInvariants(); err != nil {
			t.Fatal(err)
		}
	})
}
