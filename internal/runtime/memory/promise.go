package memory

// PromiseState is monotonic: Pending may become Fulfilled or Rejected exactly
// once.
type PromiseState uint8

const (
	PromisePending PromiseState = iota
	PromiseFulfilled
	PromiseRejected
)

// PromiseReaction retains optional Function handlers and an optional
// downstream Promise. Each field is null or the named heap kind.
type PromiseReaction struct {
	OnFulfilled Value
	OnRejected  Value
	Downstream  Value
}

// Promise stores settlement and reactions but performs no scheduling. Realm
// code explicitly drains reactions and queues their Functions.
type Promise struct {
	ObjectHeader
	State     PromiseState
	Result    Value
	Reactions []PromiseReaction
	Handled   bool
}

type PromiseSettlement struct {
	State     PromiseState
	Result    Value
	Reactions []PromiseReaction
}

func clonePromise(promise Promise) Promise {
	return Promise{
		ObjectHeader: cloneObjectHeader(promise.ObjectHeader),
		State:        promise.State,
		Result:       promise.Result,
		Reactions:    append([]PromiseReaction(nil), promise.Reactions...),
		Handled:      promise.Handled,
	}
}
