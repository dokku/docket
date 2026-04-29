package tasks

// RegisteredValue is the shape stored in the apply / plan run-wide
// registered map for a `register: <name>` task. Predicates reach it via
// `.registered.<name>`.
//
// For non-loop tasks, the embedded TaskOutputState is the post-override
// result and Results is nil; predicates use `.registered.foo.Changed`,
// `.registered.foo.Error`, etc. directly.
//
// For loop expansions, every iteration shares the same Register name.
// Results carries the per-iteration post-override states in source
// order. The embedded TaskOutputState is an Ansible-style aggregate
// across the iterations:
//
//   - Changed: true when any iteration's Changed is true.
//   - Error: the first non-nil error among iterations (nil when every
//     iteration succeeded).
//   - State / DesiredState / Stdout / Stderr / ExitCode / Commands:
//     the last iteration's values.
//
// Embedded-struct field access works through expr-lang/expr because
// it uses reflect.Value.FieldByName, which traverses embedded fields.
type RegisteredValue struct {
	TaskOutputState
	Results []TaskOutputState
}

// AggregateRegistered builds a RegisteredValue from one or more
// per-iteration TaskOutputStates. Callers use it to roll a loop's
// expansions into a single registered value. Passing a single state
// yields a RegisteredValue whose Results is nil and whose embedded
// fields mirror the input directly.
func AggregateRegistered(iter []TaskOutputState) RegisteredValue {
	if len(iter) == 0 {
		return RegisteredValue{}
	}
	if len(iter) == 1 {
		return RegisteredValue{TaskOutputState: iter[0]}
	}
	last := iter[len(iter)-1]
	out := RegisteredValue{
		TaskOutputState: TaskOutputState{
			DesiredState: last.DesiredState,
			State:        last.State,
			Stdout:       last.Stdout,
			Stderr:       last.Stderr,
			ExitCode:     last.ExitCode,
			Commands:     last.Commands,
			Message:      last.Message,
		},
		Results: append([]TaskOutputState(nil), iter...),
	}
	for _, s := range iter {
		if s.Changed {
			out.Changed = true
		}
		if out.Error == nil && s.Error != nil {
			out.Error = s.Error
		}
	}
	return out
}
