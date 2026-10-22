package tasks

// A consumer that checks a task before every value is known - a Terraform
// provider at plan time, where an attribute set from another resource's
// output has no value yet - names those fields with WithUnknown (#643). Each
// field is then one of three things: known and set, known and unset, or
// unknown. A rule is reported only when the known fields alone decide it; an
// unknown field counts as set wherever a rule needs a field present, and a
// rule whose outcome turns on an unknown value is skipped.

// ConstraintOption configures a ConstraintViolations check.
//
//sdk:export
type ConstraintOption func(*constraintCheck)

// WithUnknown is a ConstraintViolations option naming the recipe key paths
// whose values are not known yet, in the form Violation.Field uses: `app`,
// `users`, `users[1]` or `users[1].hash`.
//
// An unknown field counts as set for a rule that needs it present: its
// required_when, a list item's required keys, an at_least_one_of or
// all_or_none_of it is in, and the fields a required_with requires. A rule
// whose outcome turns on its value is skipped rather than reported: its
// forbidden_when, an exactly_one_of with at most one known field set, which
// it might complete or break, a conflicts_with it is in, and a required_with
// it is the Field of. A rule the known fields decide on their own is
// reported as without the option: two set fields in an exactly_one_of or a
// conflicts_with, or a set and an unset field in an all_or_none_of, whatever
// the unknown ones hold.
//
// An unknown `state` leaves every rule scoped to a state undecided, so they
// are all skipped: each field's required_when and forbidden_when and every
// constraint that lists States, the task's own and its list items'. Rules
// that apply in every state are still checked. An unknown `property` skips
// the check that a property task's property is not one another task manages.
//
// An unknown structured list (`users`) is unknown as a field, and none of its
// items are checked, since neither how many there are nor what they hold is
// known; an unknown item (`users[1]`) is skipped the same way while the other
// items are checked. A path naming no field of the task is ignored.
//
//sdk:export
func WithUnknown(paths ...string) ConstraintOption {
	return func(check *constraintCheck) {
		if check.unknown == nil {
			check.unknown = make(map[string]bool, len(paths))
		}
		for _, path := range paths {
			check.unknown[path] = true
		}
	}
}

// constraintCheck is what the ConstraintOptions of one ConstraintViolations
// call configure. A nil check knows every value.
type constraintCheck struct {
	unknown map[string]bool
}

// newConstraintCheck applies opts to a fresh check.
func newConstraintCheck(opts []ConstraintOption) *constraintCheck {
	check := &constraintCheck{}
	for _, opt := range opts {
		if opt != nil {
			opt(check)
		}
	}
	return check
}

// isUnknown reports whether the value at the recipe key path is unknown.
func (check *constraintCheck) isUnknown(path string) bool {
	return check != nil && check.unknown[path]
}

// fieldStatus is what a check knows about one field.
type fieldStatus int

const (
	fieldUnset fieldStatus = iota
	fieldSet
	fieldUnknown
)
