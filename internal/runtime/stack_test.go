package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestStackVisitsBorrowedFramesAndEnforcesLIFO(t *testing.T) {
	refs := []memory.Ref{
		{Region: 1, Slot: 1, Gen: 1},
		{Region: 2, Slot: 2, Gen: 2},
		{Region: 3, Slot: 3, Gen: 3},
	}
	first := &Frame{Function: refs[0], Arguments: []memory.Value{memory.RefValue(refs[1])}}
	second := &Frame{This: memory.RefValue(refs[2])}
	stack := &Stack{}
	if err := stack.push(first); err != nil {
		t.Fatal(err)
	}
	if err := stack.push(second); err != nil {
		t.Fatal(err)
	}
	if stack.Depth() != 2 {
		t.Fatalf("Depth() = %d, want 2", stack.Depth())
	}
	var visited []memory.Ref
	if err := stack.VisitRefs(func(ref memory.Ref) error {
		visited = append(visited, ref)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(visited, refs) {
		t.Fatalf("VisitRefs() = %#v, want %#v", visited, refs)
	}
	if err := stack.pop(first); !errors.Is(err, ErrFrameStackOrder) {
		t.Fatalf("out-of-order pop error = %v", err)
	}
	if err := stack.pop(second); err != nil {
		t.Fatal(err)
	}
	if err := stack.pop(first); err != nil {
		t.Fatal(err)
	}
	if err := stack.pop(first); !errors.Is(err, ErrFrameStackUnderflow) {
		t.Fatalf("empty pop error = %v", err)
	}
	if err := stack.finish(); err != nil {
		t.Fatal(err)
	}
	if err := stack.push(first); !errors.Is(err, ErrFinishedStack) {
		t.Fatalf("push after finish error = %v", err)
	}
}

func TestStackReusesClearedFrameStorage(t *testing.T) {
	stack := &Stack{}
	frame := stack.acquireFrame()
	frame.Function = memory.Ref{Region: 1, Slot: 2, Gen: 3}
	frame.Arguments = append(frame.Arguments, memory.RefValue(frame.Function))
	frame.Stack = append(frame.Stack, memory.RefValue(frame.Function))
	if err := stack.push(frame); err != nil {
		t.Fatal(err)
	}
	if err := stack.pop(frame); err != nil {
		t.Fatal(err)
	}
	stack.recycleFrame(frame)
	reused := stack.acquireFrame()
	if reused != frame {
		t.Fatalf("acquireFrame returned %p, want reused %p", reused, frame)
	}
	var roots int
	if err := reused.VisitRefs(func(memory.Ref) error { roots++; return nil }); err != nil {
		t.Fatal(err)
	}
	if roots != 0 || len(reused.Arguments) != 0 || len(reused.Stack) != 0 {
		t.Fatalf("reused frame retained roots=%d arguments=%d stack=%d", roots, len(reused.Arguments), len(reused.Stack))
	}
	stack.recycleFrame(reused)
	if allocations := testing.AllocsPerRun(100, func() {
		candidate := stack.acquireFrame()
		if err := stack.push(candidate); err != nil {
			panic(err)
		}
		if err := stack.pop(candidate); err != nil {
			panic(err)
		}
		stack.recycleFrame(candidate)
	}); allocations != 0 {
		t.Fatalf("reused frame push/pop allocated %.2f objects", allocations)
	}
}

func TestFrameVisitRefsDoesNotAllocateOrChangeOwnership(t *testing.T) {
	ledger := ownership.NewLedger()
	before := ledger.Stats()
	ref := memory.Ref{Region: 7, Slot: 8, Gen: 9}
	frame := &Frame{Function: ref, Stack: []memory.Value{memory.RefValue(ref)}}
	allocations := testing.AllocsPerRun(100, func() {
		if err := frame.VisitRefs(func(memory.Ref) error { return nil }); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("Frame.VisitRefs allocations = %f, want 0", allocations)
	}
	if after := ledger.Stats(); after != before {
		t.Fatalf("borrowed frame changed ownership: before=%#v after=%#v", before, after)
	}
}

func TestTaskContextVisitsIntrinsicOnlyRootsWithoutAllocating(t *testing.T) {
	global := memory.Ref{Region: 11, Slot: 12, Gen: 13}
	prototype := memory.Ref{Region: 14, Slot: 15, Gen: 16}
	context := &TaskContext{intrinsics: &Intrinsics{Global: global, ObjectPrototype: prototype}}
	var visited []memory.Ref
	if err := context.VisitRefs(func(ref memory.Ref) error {
		visited = append(visited, ref)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(visited, []memory.Ref{global, prototype}) {
		t.Fatalf("VisitRefs() = %#v, want intrinsic roots", visited)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := context.VisitRefs(func(memory.Ref) error { return nil }); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatalf("TaskContext.VisitRefs intrinsic traversal allocated %.2f objects", allocations)
	}
}

func TestTaskCompletionRejectsActiveFramesAndStillReleasesRegion(t *testing.T) {
	realm, err := NewRealm(9901, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()

	id := TaskID(9902)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	var allocated memory.Ref
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		var allocErr error
		allocated, allocErr = context.NewCell()
		if allocErr != nil {
			return allocErr
		}
		return context.stack.push(&Frame{Function: allocated})
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.execute(task); !errors.Is(err, ErrActiveFrames) {
		t.Fatalf("execute error = %v, want ErrActiveFrames", err)
	}
	if _, err := realm.store.Deref(owner, allocated); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("active-frame allocation survived task release: %v", err)
	}
}

func TestInterpreterRegistersNativeAndBytecodeFrames(t *testing.T) {
	realm, err := NewRealm(9903, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{})
	depth := 0
	if err := interpreter.RegisterNative(9904, func(context *TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
		depth = context.StackDepth()
		return memory.NumberValue(42), nil
	}); err != nil {
		t.Fatal(err)
	}
	id := TaskID(9905)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		native, err := context.NewNativeFunction(memory.NullValue(), memory.NullValue(), 0, 9904)
		if err != nil {
			return err
		}
		caller, err := context.NewBytecodeFunction(
			memory.NullValue(), memory.NullValue(), 0,
			Assemble(
				Instruction{Op: OpConstant, A: 0},
				Instruction{Op: OpCall},
				Instruction{Op: OpReturn},
			),
			[]memory.Value{memory.RefValue(native)},
		)
		if err != nil {
			return err
		}
		result, err := interpreter.Execute(context, caller)
		if err != nil {
			return err
		}
		if result.Number() != 42 {
			t.Fatalf("result = %#v, want 42", result)
		}
		if context.StackDepth() != 0 {
			t.Fatalf("stack depth after execution = %d", context.StackDepth())
		}
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.execute(task); err != nil {
		t.Fatal(err)
	}
	if depth != 2 {
		t.Fatalf("nested native depth = %d, want 2", depth)
	}
}

func TestReentrantInterpreterCallsShareTaskCallDepth(t *testing.T) {
	realm, err := NewRealm(9913, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{MaxCallDepth: 3})
	var function memory.Ref
	maxDepth := 0
	if err := interpreter.RegisterNative(9914, func(context *TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
		if depth := context.StackDepth(); depth > maxDepth {
			maxDepth = depth
		}
		return interpreter.CallWithoutCheckpoint(context, function, memory.UndefinedValue())
	}); err != nil {
		t.Fatal(err)
	}
	id := TaskID(9915)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		var allocErr error
		function, allocErr = context.NewNativeFunction(memory.NullValue(), memory.NullValue(), 0, 9914)
		if allocErr != nil {
			return allocErr
		}
		_, callErr := interpreter.Execute(context, function)
		if !errors.Is(callErr, ErrCallDepth) {
			return fmt.Errorf("reentrant call error = %v, want ErrCallDepth", callErr)
		}
		if context.StackDepth() != 0 {
			return fmt.Errorf("stack depth after reentrant error = %d", context.StackDepth())
		}
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.execute(task); err != nil {
		t.Fatal(err)
	}
	if maxDepth != 3 {
		t.Fatalf("maximum reentrant stack depth = %d, want 3", maxDepth)
	}
}

func TestDerivedTaskScopesShareBorrowedStack(t *testing.T) {
	realm, err := NewRealm(9906, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	id := TaskID(9907)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, func(*TaskContext) error { return nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	context := &TaskContext{
		Realm: realm, TaskID: id, Owner: owner, Region: task.region,
		MemoryRegion: task.memoryRegion, stack: &Stack{},
	}
	frame := &Frame{Function: memory.Ref{Region: task.memoryRegion, Slot: 1, Gen: 1}}
	if err := context.stack.push(frame); err != nil {
		t.Fatal(err)
	}
	derived, err := context.WithMemoryRegion(task.memoryRegion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if derived.stack != context.stack || derived.StackDepth() != 1 {
		t.Fatalf("derived context stack = %p depth %d, want %p depth 1", derived.stack, derived.StackDepth(), context.stack)
	}
	realmRegion, err := realm.store.NewRegion(realm.owner)
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := context.WithBorrowedRealmMemoryRegion(realmRegion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if borrowed.stack != context.stack || borrowed.StackDepth() != 1 {
		t.Fatalf("borrowed context stack = %p depth %d, want %p depth 1", borrowed.stack, borrowed.StackDepth(), context.stack)
	}
	if err := context.stack.pop(frame); err != nil {
		t.Fatal(err)
	}
	if err := realm.store.ReleaseOwner(owner); err != nil {
		t.Fatal(err)
	}
}

func TestFailedMicrotaskCheckpointDiscardsBorrowedJobs(t *testing.T) {
	realm, err := NewRealm(9908, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{MaxMicrotasks: 1})
	taskID := TaskID(9909)
	context := &TaskContext{Realm: realm, TaskID: taskID, stack: &Stack{}}
	interpreter.jobs[taskID] = []microtaskJob{
		{kind: microtaskJobKind(255)},
		{kind: microtaskJobKind(255)},
	}
	if err := interpreter.DrainJobs(context); !errors.Is(err, ErrMicrotaskLimit) {
		t.Fatalf("DrainJobs error = %v, want ErrMicrotaskLimit", err)
	}
	if jobs := interpreter.jobs[taskID]; len(jobs) != 0 {
		t.Fatalf("failed checkpoint retained %d borrowed jobs", len(jobs))
	}
}

func TestTaskContextVisitsPendingMicrotaskRoots(t *testing.T) {
	refs := []memory.Ref{
		{Region: 1, Slot: 1, Gen: 1},
		{Region: 2, Slot: 2, Gen: 2},
		{Region: 3, Slot: 3, Gen: 3},
		{Region: 4, Slot: 4, Gen: 4},
		{Region: 5, Slot: 5, Gen: 5},
		{Region: 6, Slot: 6, Gen: 6},
		{Region: 7, Slot: 7, Gen: 7},
	}
	interpreter := NewInterpreter(InterpreterConfig{})
	context := &TaskContext{
		TaskID: 9910,
		Refs:   []memory.Ref{refs[0]},
		stack:  &Stack{},
		jobs:   &taskJobs{},
	}
	frame := &Frame{Function: refs[1]}
	if err := context.stack.push(frame); err != nil {
		t.Fatal(err)
	}
	execution := &execution{interpreter: interpreter, context: context}
	execution.enqueueJob(microtaskJob{
		kind:     microtaskPromiseReaction,
		callback: refs[2],
		result:   memory.RefValue(refs[3]),
		reaction: memory.PromiseReaction{
			OnFulfilled: memory.RefValue(refs[4]),
			OnRejected:  memory.RefValue(refs[5]),
			Downstream:  memory.RefValue(refs[6]),
		},
	})

	var visited []memory.Ref
	if err := context.VisitRefs(func(ref memory.Ref) error {
		visited = append(visited, ref)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(visited, refs) {
		t.Fatalf("VisitRefs() = %#v, want %#v", visited, refs)
	}
	interpreter.DiscardJobs(context.TaskID)
}

func TestTaskContextJobVisitorMayReenterInterpreter(t *testing.T) {
	interpreter := NewInterpreter(InterpreterConfig{})
	context := &TaskContext{TaskID: 9916, stack: &Stack{}, jobs: &taskJobs{}}
	execution := &execution{interpreter: interpreter, context: context}
	execution.enqueueJob(microtaskJob{
		kind:     microtaskCallback,
		callback: memory.Ref{Region: 1, Slot: 2, Gen: 3},
	})
	done := make(chan error, 1)
	go func() {
		done <- context.VisitRefs(func(memory.Ref) error {
			interpreter.DiscardJobs(context.TaskID)
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("VisitRefs deadlocked while visitor re-entered Interpreter")
	}
}

func TestActivePromiseReactionRemainsABorrowedRoot(t *testing.T) {
	realm, err := NewRealm(9917, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{})
	var downstream memory.Ref
	observed := false
	if err := interpreter.RegisterNative(9918, func(context *TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
		if err := context.VisitRefs(func(ref memory.Ref) error {
			if ref == downstream {
				observed = true
			}
			return nil
		}); err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	id := TaskID(9919)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		handler, err := context.NewNativeFunction(memory.NullValue(), memory.NullValue(), 0, 9918)
		if err != nil {
			return err
		}
		downstream, err = context.NewPromise()
		if err != nil {
			return err
		}
		result, err := context.NewCell()
		if err != nil {
			return err
		}
		execution := &execution{interpreter: interpreter, context: context}
		execution.enqueueJob(microtaskJob{
			kind:   microtaskPromiseReaction,
			state:  memory.PromiseFulfilled,
			result: memory.RefValue(result),
			reaction: memory.PromiseReaction{
				OnFulfilled: memory.RefValue(handler),
				Downstream:  memory.RefValue(downstream),
			},
		})
		return interpreter.DrainJobs(context)
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.execute(task); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("active promise reaction downstream was absent from borrowed roots")
	}
}

func TestTaskCompletionRejectsAndDiscardsPendingMicrotasks(t *testing.T) {
	realm, err := NewRealm(9911, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{})
	id := TaskID(9912)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	var callback memory.Ref
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		var allocErr error
		callback, allocErr = context.NewCell()
		if allocErr != nil {
			return allocErr
		}
		execution := &execution{interpreter: interpreter, context: context}
		execution.enqueueJob(microtaskJob{kind: microtaskCallback, callback: callback})
		return nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.execute(task); !errors.Is(err, ErrPendingMicrotasks) {
		t.Fatalf("execute error = %v, want ErrPendingMicrotasks", err)
	}
	if interpreter.hasTaskJobs(id) {
		t.Fatal("task completion retained pending microtasks")
	}
	if _, err := realm.store.Deref(owner, callback); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("pending microtask allocation survived task release: %v", err)
	}
}

func BenchmarkInterpreterNativeFrameReuse(b *testing.B) {
	realm, err := NewRealm(9920, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer realm.Close()
	interpreter := NewInterpreter(InterpreterConfig{})
	if err := interpreter.RegisterNative(9921, func(*TaskContext, memory.Value, []memory.Value) (memory.Value, error) {
		return memory.UndefinedValue(), nil
	}); err != nil {
		b.Fatal(err)
	}
	id := TaskID(9922)
	owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(id)}
	task, err := realm.newTask(id, owner, func(context *TaskContext) error {
		function, err := context.NewNativeFunction(memory.NullValue(), memory.NullValue(), 0, 9921)
		if err != nil {
			return err
		}
		if _, err := interpreter.Execute(context, function); err != nil {
			return err
		}
		b.ResetTimer()
		for iteration := 0; iteration < b.N; iteration++ {
			if _, err := interpreter.Execute(context, function); err != nil {
				return err
			}
		}
		b.StopTimer()
		return nil
	}, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	if err := realm.execute(task); err != nil {
		b.Fatal(err)
	}
}
