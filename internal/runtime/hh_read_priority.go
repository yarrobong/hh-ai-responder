package runtime

import "context"

// HH reads share one global interval limiter. Priority only chooses which
// waiting read gets the next slot; it never creates an extra slot or bypasses
// the interval.
type hhReadPriority int

const (
	hhReadBackground hhReadPriority = iota + 1
	hhReadForegroundInbox
	hhReadTargeted
	hhReadSafety
)

type hhReadPriorityContextKey struct{}

func withHHReadPriority(ctx context.Context, priority hhReadPriority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, hhReadPriorityContextKey{}, priority)
}

func hhReadPriorityFromContext(ctx context.Context) hhReadPriority {
	if ctx != nil {
		if priority, ok := ctx.Value(hhReadPriorityContextKey{}).(hhReadPriority); ok && priority > 0 {
			return priority
		}
	}
	return 0
}
