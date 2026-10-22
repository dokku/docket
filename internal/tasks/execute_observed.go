package tasks

import (
	"context"
	"reflect"
)

// skipObservedKey is the context key ContextWithoutObserved sets.
type skipObservedKey struct{}

// ContextWithoutObserved returns a copy of ctx on which ExecutePlan, and so
// Execute, leaves TaskOutputState.Observed and Exists unset and never
// re-probes the server for them. `docket apply` runs its tasks under it: it
// reports and registers what a task did, not what the server now holds, so
// the extra read would be wasted. An embedder that never reads Observed can
// do the same.
//
//sdk:export
func ContextWithoutObserved(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipObservedKey{}, true)
}

// observes reports whether ExecutePlan fills the observed state for p on ctx:
// not on a context from ContextWithoutObserved, and not for an imperative
// task, which runs an operation rather than declaring a resource, so what it
// leaves on the server is not a state to report.
func observes(ctx context.Context, p PlanResult) bool {
	if skip, _ := ctx.Value(skipObservedKey{}).(bool); skip {
		return false
	}
	return p.planned != nil && !TaskImperative(p.planned)
}

// appliedObserved returns the task as the server holds it after p's apply
// succeeded, and whether the resource exists, by the rules PlanResult.Observed
// follows for a plan in sync: every field the planned task declares as
// declared, every field it leaves unset from the server, and every write_only
// or apply_only field left unread.
//
// The plan's own read is reused when the planned task declares every field a
// read fills, since the read then contributes nothing the declaration does
// not override. Otherwise the planned task is planned again, because the apply
// may have moved a field the task leaves to the server: a create picks the
// server's defaults (the image a dokku_service_create without one ends up
// on), a destroy or a clear removes what the read held, and an in-place change
// may reset what it does not pass, as a dokku_storage_mount remount does every
// mount option the task leaves out. A task whose plan read nothing (one whose probe cannot read
// its state, or a probe failure the plan treated as drift) observes nothing,
// and so does one whose re-probe fails.
func appliedObserved(ctx context.Context, p PlanResult) (Task, bool) {
	if p.planned == nil || p.read == nil {
		return nil, false
	}
	if !leavesReadFieldUnset(valueOf(p.planned)) {
		obs := declaredObserved(p.planned, p.read)
		return obs, declaresPresent(obs)
	}
	// The second plan is in sync after an apply that did its job, so its
	// Observed follows the in-sync rules. One that still reports drift
	// observes what the server holds, which is the more useful answer than
	// the declaration the apply did not reach.
	again := p.planned.Plan(ctx)
	if again.Error != nil {
		return nil, false
	}
	return again.Observed, again.Exists
}

// leavesReadFieldUnset reports whether the struct v leaves at its zero value
// a field a read fills, which is what declaredObserved takes from the read
// rather than from the declaration. Identity keys are always the planned
// task's, and write_only and apply_only fields are never read, so neither
// counts. It descends the way fillUnset does: into nested structs, and into
// the elements of a list of structs with identity keys.
func leavesReadFieldUnset(v reflect.Value) bool {
	if v.Kind() != reflect.Struct {
		return false
	}
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() || field.Tag.Get("yaml") == "-" {
			continue
		}
		if field.Tag.Get(identityTag) == identityTagKey || field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			continue
		}
		fv := v.Field(i)
		if isUnset(fv) {
			return true
		}
		switch fv.Kind() {
		case reflect.Struct:
			if leavesReadFieldUnset(fv) {
				return true
			}
		case reflect.Slice:
			if len(itemKeyFields(fv.Type().Elem())) == 0 {
				continue
			}
			for j := 0; j < fv.Len(); j++ {
				if leavesReadFieldUnset(fv.Index(j)) {
					return true
				}
			}
		}
	}
	return false
}
