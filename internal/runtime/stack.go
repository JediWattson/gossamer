package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

var (
	ErrFrameStackUnderflow = errors.New("runtime: frame stack underflow")
	ErrFrameStackOrder     = errors.New("runtime: frame stack pop out of order")
	ErrActiveFrames        = errors.New("runtime: task completed with active frames")
	ErrFinishedStack       = errors.New("runtime: task stack is finished")
	ErrPendingMicrotasks   = errors.New("runtime: task completed with pending microtasks")
)

type taskJobs struct {
	interpreters []*Interpreter
	active       []microtaskJob
}

// Stack is one task's synchronous execution stack. Frames and their Values are
// borrowed roots: visiting or pushing them never creates an ownership claim.
// A Stack is confined to its Realm's ordered executor.
type Stack struct {
	frames     []*Frame
	freeFrames []*Frame
	finished   bool
}

// StackDepth reports the number of active synchronous calls in this task.
func (context *TaskContext) StackDepth() int {
	if context == nil {
		return 0
	}
	return context.stack.Depth()
}

// VisitRefs visits host-held intrinsics, queue-delivered refs, active frames,
// and task jobs without retaining, publishing, transferring, or otherwise
// changing their ownership.
func (context *TaskContext) VisitRefs(visit func(memory.Ref) error) error {
	if context == nil || visit == nil {
		return nil
	}
	if err := context.intrinsics.VisitRefs(visit); err != nil {
		return err
	}
	for _, ref := range context.Refs {
		if ref == (memory.Ref{}) {
			continue
		}
		if err := visit(ref); err != nil {
			return err
		}
	}
	if err := context.stack.VisitRefs(visit); err != nil {
		return err
	}
	if context.jobs == nil {
		return nil
	}
	for index := range context.jobs.active {
		if err := visitMicrotaskJobRefs(&context.jobs.active[index], visit); err != nil {
			return err
		}
	}
	for _, interpreter := range context.jobs.interpreters {
		if err := interpreter.visitJobRefs(context.TaskID, visit); err != nil {
			return err
		}
	}
	return nil
}

func (context *TaskContext) finishStack() error {
	if context == nil {
		return nil
	}
	return context.stack.finish()
}

func (context *TaskContext) trackJobs(interpreter *Interpreter) {
	if context == nil || interpreter == nil {
		return
	}
	if context.jobs == nil {
		context.jobs = &taskJobs{}
	}
	for _, tracked := range context.jobs.interpreters {
		if tracked == interpreter {
			return
		}
	}
	context.jobs.interpreters = append(context.jobs.interpreters, interpreter)
}

func (context *TaskContext) finishJobs() error {
	if context == nil || context.jobs == nil {
		return nil
	}
	pending := 0
	for _, interpreter := range context.jobs.interpreters {
		if !interpreter.hasTaskJobs(context.TaskID) {
			continue
		}
		pending++
		interpreter.DiscardJobs(context.TaskID)
	}
	clear(context.jobs.interpreters)
	context.jobs.interpreters = nil
	active := len(context.jobs.active)
	clear(context.jobs.active)
	context.jobs.active = nil
	if pending != 0 || active != 0 {
		return fmt.Errorf("%w: %d active jobs, %d interpreter queues", ErrPendingMicrotasks, active, pending)
	}
	return nil
}

func (context *TaskContext) beginJob(job microtaskJob) {
	if context.jobs == nil {
		context.jobs = &taskJobs{}
	}
	context.jobs.active = append(context.jobs.active, job)
}

func (context *TaskContext) finishJob() error {
	if context == nil || context.jobs == nil || len(context.jobs.active) == 0 {
		return fmt.Errorf("runtime: active microtask stack underflow")
	}
	index := len(context.jobs.active) - 1
	context.jobs.active[index] = microtaskJob{}
	context.jobs.active = context.jobs.active[:index]
	return nil
}

func (stack *Stack) Depth() int {
	if stack == nil {
		return 0
	}
	return len(stack.frames)
}

func (stack *Stack) VisitRefs(visit func(memory.Ref) error) error {
	if stack == nil || visit == nil {
		return nil
	}
	for _, frame := range stack.frames {
		if err := frame.VisitRefs(visit); err != nil {
			return err
		}
	}
	return nil
}

func (stack *Stack) push(frame *Frame) error {
	if stack == nil {
		return fmt.Errorf("runtime: nil task stack")
	}
	if stack.finished {
		return ErrFinishedStack
	}
	if frame == nil {
		return fmt.Errorf("runtime: nil frame")
	}
	stack.frames = append(stack.frames, frame)
	return nil
}

func (stack *Stack) pop(frame *Frame) error {
	if stack == nil || len(stack.frames) == 0 {
		return ErrFrameStackUnderflow
	}
	index := len(stack.frames) - 1
	if stack.frames[index] != frame {
		return ErrFrameStackOrder
	}
	stack.frames[index] = nil
	stack.frames = stack.frames[:index]
	return nil
}

func (stack *Stack) acquireFrame() *Frame {
	if stack == nil || len(stack.freeFrames) == 0 {
		return &Frame{}
	}
	index := len(stack.freeFrames) - 1
	frame := stack.freeFrames[index]
	stack.freeFrames[index] = nil
	stack.freeFrames = stack.freeFrames[:index]
	return frame
}

func (stack *Stack) recycleFrame(frame *Frame) {
	if stack == nil || frame == nil || stack.finished {
		return
	}
	frame.reset()
	stack.freeFrames = append(stack.freeFrames, frame)
}

func (stack *Stack) finish() error {
	if stack == nil {
		return nil
	}
	active := len(stack.frames)
	clear(stack.frames)
	stack.frames = nil
	for _, frame := range stack.freeFrames {
		frame.reset()
	}
	clear(stack.freeFrames)
	stack.freeFrames = nil
	stack.finished = true
	if active != 0 {
		return fmt.Errorf("%w: %d", ErrActiveFrames, active)
	}
	return nil
}
