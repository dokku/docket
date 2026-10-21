package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrObjectMissing is matched by the error a read returns when the app or
// service a task depends on does not exist on the server. errors.As against
// *ObjectMissingError names the missing object.
var ErrObjectMissing = errors.New("an object the task depends on does not exist")

// ObjectMissingError reports that an object a task depends on - its app or
// its datastore service - does not exist on the server. It matches
// ErrObjectMissing under errors.Is.
type ObjectMissingError struct {
	// Object is the missing object, with the recipe keys that name it and
	// their values on the task.
	Object TaskObject
}

// Error names the missing object the way dokku does: `app web does not
// exist`, or `service postgres db does not exist`.
func (e *ObjectMissingError) Error() string {
	return fmt.Sprintf("%s %s does not exist", e.Object.Kind, strings.Join(e.Object.Values, " "))
}

// Is reports whether target is ErrObjectMissing.
func (e *ObjectMissingError) Is(target error) bool {
	return target == ErrObjectMissing
}

// objectMissing returns the *ObjectMissingError for t's object of kind, for a
// plan that has found that object missing itself.
func objectMissing(t Task, kind ObjectKind) error {
	for _, object := range TaskObjects(t) {
		if object.Kind == kind {
			return &ObjectMissingError{Object: object}
		}
	}
	return fmt.Errorf("%s does not exist", kind)
}

// objectExists probes the server for object.
func objectExists(ctx context.Context, object TaskObject) (bool, error) {
	switch object.Kind {
	case ObjectApp:
		return appExists(ctx, object.Values[0])
	case ObjectService:
		return serviceExists(ctx, object.Values[0], object.Values[1])
	}
	return false, fmt.Errorf("unknown object kind %q", object.Kind)
}

// missingObject returns an *ObjectMissingError for the first object t depends
// on that does not exist, in TaskObjects order. An object t creates, or one t
// does not name, is not a dependency. It returns nil when every object exists
// or when a probe fails, so the caller keeps the error it already has.
func missingObject(ctx context.Context, t Task) error {
	for _, object := range TaskObjects(t) {
		if !object.Set || object.Creates {
			continue
		}
		exists, err := objectExists(ctx, object)
		if err != nil {
			return nil
		}
		if !exists {
			return &ObjectMissingError{Object: object}
		}
	}
	return nil
}

// planRead reads t from one Plan, as ReadTask does. A task whose inputs are
// invalid returns the plan's refusal, never a missing object.
func planRead(ctx context.Context, t Task) (Task, bool, error) {
	got, exists, err := observedResult(t.Plan(ctx))
	if err != nil && checkTaskInputs(t) != nil {
		return got, exists, err
	}
	return readResult(ctx, t, got, exists, err)
}

// readResult is what ReadTask and ReadAddress return for t once its own read
// is done. A read that did not find the resource present asks the server
// whether the app or service t depends on exists, and reports a missing one
// as an *ObjectMissingError in place of the read's own result. A present
// read, a version refusal and a cancelled context come back as they are.
func readResult(ctx context.Context, t Task, got Task, exists bool, err error) (Task, bool, error) {
	if err == nil && exists {
		return got, exists, err
	}
	var missing *ObjectMissingError
	var verr *VersionError
	if errors.As(err, &missing) || errors.As(err, &verr) || ctx.Err() != nil {
		return got, exists, err
	}
	if missingErr := missingObject(ctx, t); missingErr != nil {
		return nil, false, missingErr
	}
	return got, exists, err
}
