# Writing tasks

This page is for contributors adding new task types to docket. If you only want to *use* tasks in a
recipe, see the [task reference](tasks/README.md) instead.

Tasks closely follow the modules available in
[`ansible-dokku`](https://github.com/dokku/ansible-dokku), so that existing task lists migrate with
minimal changes. docket adds a few of its own tasks specific to this package. A task has a name and
an execution context that maps to a single module; its fields can be templated from recipe
[inputs](inputs.md) and any function [sigil](https://github.com/gliderlabs/sigil) exposes.

## The Plan / Execute model

Every task implements two methods. `Plan()` is the canonical one: it probes the live server once,
computes the difference between the current and desired state, and returns a `PlanResult`. When the
server is not already in the desired state, `Plan()` embeds an `apply` closure that performs the
mutation. `Execute()` is always just `ExecutePlan(t.Plan())` - the shared `ExecutePlan` helper
handles the in-sync, error, and apply cases uniformly, so the mutation logic lives in exactly one
place per task.

This split is why `plan` and `apply` agree: both call the same `Plan()`. `apply` reuses the probe
to decide whether to mutate, which is what makes back-to-back applies no-ops.

## Adding a new task

Create `tasks/${TASK_NAME}_task.go`, where the task name is `lower_underscore_case`. For a task
named `lollipop`, `tasks/lollipop_task.go` would contain:

```go
package main

type LollipopTask struct {
  App   string `required:"true" yaml:"app"`
  State State  `required:"true" yaml:"state" default:"present"`
}

func (t LollipopTask) Plan() PlanResult {
  return DispatchPlan(t.State, map[State]func() PlanResult{
    "present": func() PlanResult {
      // Probe the server once, decide whether to mutate.
      if /* already in desired state */ {
        return PlanResult{InSync: true, Status: PlanStatusOK}
      }
      return PlanResult{
        InSync:    false,
        Status:    PlanStatusCreate, // or PlanStatusModify, PlanStatusDestroy
        Reason:    "...",
        Mutations: []string{"create lollipop"},
        apply: func() TaskOutputState {
          // Run the underlying dokku command. Return Changed=true on success.
          return TaskOutputState{Changed: true, State: StatePresent}
        },
      }
    },
    "absent": func() PlanResult { /* ... */ },
  })
}

func (t LollipopTask) Execute() TaskOutputState {
  return ExecutePlan(t.Plan())
}

func init() {
  RegisterTask(&LollipopTask{})
}
```

A few conventions to follow:

- The struct holds the fields the task needs. The only required field is `State`, the desired state;
  everything else is specific to the task.
- For a task that performs several atomic changes in one call (such as setting multiple config
  keys), populate `PlanResult.Mutations` with one entry per change, so `plan` can itemize the diff.
- `DispatchPlan` and `DispatchState` set `DesiredState` on the result automatically.
- `init()` registers the task with `RegisterTask`, which makes it usable in a recipe.

## Regenerating the task docs

The per-task pages under [`docs/tasks/`](tasks/README.md) are generated from each task's `Doc()` and
`Examples()` methods - they are not hand-edited. After adding or changing a task, regenerate them:

```bash
make docs
```

This runs `go generate generate/docs.go`, which writes one `docs/tasks/<task>.md` per registered
task plus the `docs/tasks/README.md` index. Commit the regenerated files alongside your code.

## See also

- [Tasks](tasks/README.md) - the generated reference for every task
- [Task envelope](task-envelope.md) - the cross-cutting keys every task supports
- [Command reference](command-reference.md) - how `plan` and `apply` consume `Plan()`
