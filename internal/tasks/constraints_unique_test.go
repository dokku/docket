package tasks

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestEveryUniqueListIsEnforced repeats a member of every list tagged unique,
// element lists included, and checks the checker reports the repeat and that
// both Validate and Plan refuse the task over it. Plan stops in gatePlan
// before any probe, so no server is involved.
func TestEveryUniqueListIsEnforced(t *testing.T) {
	t.Parallel()
	total := 0
	for _, typeKey := range TaskTypes() {
		prototype, _ := Lookup(typeKey)
		taskType := taskStructOf(prototype)
		for _, s := range declaredStructs(taskType, typeKey) {
			list := ""
			if s.rt != taskType {
				list = strings.TrimSuffix(strings.TrimPrefix(s.where, typeKey+"."), "[]")
			}
			for i := 0; i < s.rt.NumField(); i++ {
				field := s.rt.Field(i)
				name := recipeKey(field)
				if name == "" || field.Tag.Get(uniqueTag) != "true" {
					continue
				}
				total++
				where := s.where + "." + name

				task := reflect.New(taskType).Elem()
				target, prefix := task, ""
				if list != "" {
					for j := 0; j < taskType.NumField(); j++ {
						if recipeKey(taskType.Field(j)) == list {
							task.Field(j).Set(reflect.MakeSlice(taskType.Field(j).Type, 1, 1))
							target, prefix = task.Field(j).Index(0), list+"[0]."
						}
					}
				}
				member := sampleValue(field.Type.Elem(), "")
				repeated := reflect.MakeSlice(field.Type, 0, 2)
				repeated = reflect.Append(repeated, member, member)
				target.Field(i).Set(repeated)

				path := prefix + name
				want := Violation{
					Field:   path + "[1]",
					Message: fmt.Sprintf("'%s[1]' repeats '%s[0]'; the members of '%s' must be unique", path, path, path),
				}
				violations := ConstraintViolations(task.Interface())
				if !slices.Contains(violations, want) {
					t.Errorf("%s: violations %+v, want one of them to be %+v", where, violations, want)
					continue
				}
				if slices.Contains(ConstraintViolations(task.Interface(), WithUnknown(path+"[1]")), want) {
					t.Errorf("%s: an unknown repeated member is still reported", where)
				}
				typed := task.Interface().(Task)
				if validator, ok := typed.(InputValidator); !ok {
					t.Errorf("%s declares a unique list but has no Validate()", typeKey)
				} else if err := validator.Validate(); err == nil {
					t.Errorf("%s: Validate() = nil, want an error", where)
				}
				if plan := typed.Plan(testCtx()); plan.Error == nil {
					t.Errorf("%s: Plan error = nil, want an error", where)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("no unique lists found")
	}
}

// TestUniqueListViolations pins what a repeat is and where it is reported:
// an exact repeat of a member, at the later member's path, naming the first
// member it repeats, and nothing for a key with two different values.
func TestUniqueListViolations(t *testing.T) {
	t.Parallel()
	repeat := func(path string, later, first int) Violation {
		return Violation{
			Field:   fmt.Sprintf("%s[%d]", path, later),
			Message: fmt.Sprintf("'%s[%d]' repeats '%s[%d]'; the members of '%s' must be unique", path, later, path, first, path),
		}
	}
	profile := func(args ...string) SchedulerK3sProfileTask {
		return SchedulerK3sProfileTask{Name: "edge", Role: "worker", KubeletArgs: args, State: StatePresent}
	}

	for _, tc := range []struct {
		name    string
		task    any
		unknown []string
		want    []Violation
	}{
		{
			name: "distinct kubelet_args",
			task: profile("max-pods=100", "image-gc-high-threshold=80"),
		},
		{
			name: "one kubelet arg key with two values is two arguments",
			task: profile("max-pods=100", "max-pods=110"),
		},
		{
			name: "an exact kubelet arg repeat",
			task: profile("max-pods=100", "image-gc-high-threshold=80", "max-pods=100"),
			want: []Violation{repeat("kubelet_args", 2, 0)},
		},
		{
			name: "every later repeat names the first member",
			task: profile("max-pods=100", "max-pods=100", "max-pods=100"),
			want: []Violation{repeat("kubelet_args", 1, 0), repeat("kubelet_args", 2, 0)},
		},
		{
			name:    "an unknown list is not checked",
			task:    profile("max-pods=100", "max-pods=100"),
			unknown: []string{"kubelet_args"},
		},
		{
			name:    "an unknown member is not compared",
			task:    profile("max-pods=100", "max-pods=100"),
			unknown: []string{"kubelet_args[1]"},
		},
		{
			name:    "known repeats are reported whatever an unknown member holds",
			task:    profile("max-pods=100", "", "max-pods=100"),
			unknown: []string{"kubelet_args[1]"},
			want:    []Violation{repeat("kubelet_args", 2, 0)},
		},
		{
			name: "a repeated preserve key",
			task: ConfigTask{App: "web", Config: map[string]string{"A": "b"}, Preserve: []string{"SECRET", "SECRET"}, State: StateSet},
			want: []Violation{repeat("preserve", 1, 0)},
		},
		{
			name: "a repeated single-mount phase",
			task: StorageMountTask{App: "web", EntryName: "data", ContainerDir: "/data", Phases: []string{"deploy", "deploy"}},
			want: []Violation{repeat("phases", 1, 0)},
		},
		{
			name: "a repeated phase on a mounts item",
			task: StorageMountTask{App: "web", State: StateSet, Mounts: []StorageMount{
				{EntryName: "data", ContainerDir: "/data", Phases: []string{"run"}},
				{EntryName: "logs", ContainerDir: "/logs", Phases: []string{"run", "deploy", "run"}},
			}},
			want: []Violation{repeat("mounts[1].phases", 2, 0)},
		},
		{
			name:    "an unknown mounts item is not checked",
			task:    StorageMountTask{App: "web", State: StateSet, Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/data", Phases: []string{"run", "run"}}}},
			unknown: []string{"mounts[0]"},
		},
		{
			name:    "an unknown member of a mounts item's phases is not compared",
			task:    StorageMountTask{App: "web", State: StateSet, Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/data", Phases: []string{"run", "run"}}}},
			unknown: []string{"mounts[0].phases[0]"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ConstraintViolations(tc.task, WithUnknown(tc.unknown...))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("violations = %+v; want %+v", got, tc.want)
			}
			if len(tc.unknown) > 0 {
				return
			}
			// Validate refuses with the first violation, and accepts a
			// task with none.
			err := tc.task.(InputValidator).Validate()
			switch {
			case len(tc.want) == 0 && err != nil:
				t.Errorf("Validate() = %v; want nil", err)
			case len(tc.want) > 0 && (err == nil || err.Error() != tc.want[0].Message):
				t.Errorf("Validate() = %v; want %q", err, tc.want[0].Message)
			}
		})
	}
}
