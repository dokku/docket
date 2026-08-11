package tasks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// ServiceCreateTask creates or destroys a dokku service.
//
// Every field below `Name` is a create-time option: the datastore plugins
// accept them as flags on `<service>:create` (the same set ansible-dokku's
// module reaches through Ansible's `environment:` keyword, e.g.
// POSTGRES_IMAGE). They are rendered onto the argv rather than passed as
// environment variables so they behave identically locally and over SSH - a
// variable put in front of the local process never reaches the remote shell,
// so the argv is the only carrier both transports share.
//
// They apply only when the service is created. The task probes
// `<service>:exists`, so a service that already exists is in sync whatever
// image it is running; changing that needs `<service>:upgrade`, which
// recreates the container and is not something docket does implicitly
// (tracked in #435).
type ServiceCreateTask struct {
	// Service is the type of service to create (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" yaml:"service" description:"Type of service to create (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the service instance"`

	// Image is the image the service container is started from.
	Image string `required:"false" yaml:"image,omitempty" description:"Image to start the service with, e.g. postgis/postgis. Applied only when the service is created."`

	// ImageVersion is the tag of the image the service container is started from.
	ImageVersion string `required:"false" yaml:"image_version,omitempty" description:"Image tag to start the service with. Applied only when the service is created."`

	// ConfigOptions are extra arguments passed to the container create command.
	ConfigOptions string `required:"false" yaml:"config_options,omitempty" description:"Extra arguments to pass to the container create command. Applied only when the service is created."`

	// CustomEnv is the environment the service container is started with.
	CustomEnv map[string]string `required:"false" sensitive:"true" yaml:"custom_env,omitempty" description:"Map of environment variables to start the service with. Applied only when the service is created."`

	// Memory is the container memory limit in megabytes.
	Memory int `required:"false" yaml:"memory,omitempty" description:"Container memory limit in megabytes. Applied only when the service is created."`

	// ShmSize is the shared memory size of the service container.
	ShmSize string `required:"false" yaml:"shm_size,omitempty" description:"Shared memory size for the service container. Applied only when the service is created."`

	// InitialNetwork is the network the service is attached to on creation.
	InitialNetwork string `required:"false" yaml:"initial_network,omitempty" description:"Network to attach the service to initially. Applied only when the service is created."`

	// PostCreateNetwork are the networks attached after service creation.
	PostCreateNetwork []string `required:"false" yaml:"post_create_network,omitempty" description:"Networks to attach the service container to after creation. Applied only when the service is created."`

	// PostStartNetwork are the networks attached after service start.
	PostStartNetwork []string `required:"false" yaml:"post_start_network,omitempty" description:"Networks to attach the service container to after start. Applied only when the service is created."`

	// Password overrides the user-level service password.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Override the user-level service password. Applied only when the service is created."`

	// RootPassword overrides the root-level service password.
	RootPassword string `required:"false" sensitive:"true" yaml:"root_password,omitempty" description:"Override the root-level service password. Applied only when the service is created."`

	// State is the desired state of the service
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the service"`
}

// ServiceCreateTaskExample contains an example of a ServiceCreateTask
type ServiceCreateTaskExample struct {
	// Name is the task name holding the ServiceCreateTask description
	Name string `yaml:"-"`

	// ServiceCreateTask is the ServiceCreateTask configuration
	ServiceCreateTask ServiceCreateTask `yaml:"dokku_service_create"`
}

// GetName returns the name of the example
func (e ServiceCreateTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service create task
func (t ServiceCreateTask) Doc() string {
	return "Creates or destroys a dokku service"
}

// ExportSupport reports how docket export handles this task.
func (t ServiceCreateTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "the service and the image it is running are exported; the remaining create-time options (config_options, custom_env, memory, shm_size, the networks, and the passwords) have no read command and must be re-supplied"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ServiceCreateTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "the service's existence and image are probed; config_options, custom_env, memory, shm_size, the networks, and the passwords have no read command and are not drift-detected"}
}

// ExportGlobal reconstructs every datastore service instance on the server as a
// dokku_service_create task. Discovery is via listServices (the `service-list`
// plugin trigger). Like dokku_storage_mount, the service's data is migrated
// separately - but the image it runs is exported, so a service recreated on
// another server comes up on the same version rather than whatever that
// server's plugin defaults to, which is what its `:import` counterpart needs.
func (t ServiceCreateTask) ExportGlobal() ([]interface{}, error) {
	services, err := listServices()
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		image, version, err := serviceImage(s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, ServiceCreateTask{
			Service:      s.Type,
			Name:         s.Name,
			Image:        image,
			ImageVersion: version,
			State:        StatePresent,
		})
	}
	return out, nil
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t ServiceCreateTask) Requirements() []string {
	return []string{"a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)"}
}

// Examples returns a list of ServiceCreateTaskExamples as yaml
func (t ServiceCreateTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ServiceCreateTaskExample{
		{
			Name: "Create a redis service named my-redis",
			ServiceCreateTask: ServiceCreateTask{
				Service: "redis",
				Name:    "my-redis",
			},
		},
		{
			Name: "Create a postgres service named my-db",
			ServiceCreateTask: ServiceCreateTask{
				Service: "postgres",
				Name:    "my-db",
			},
		},
		{
			Name: "Create a redis service on a pinned image",
			ServiceCreateTask: ServiceCreateTask{
				Service:      "redis",
				Name:         "my-pinned-redis",
				Image:        "redis",
				ImageVersion: "7.2.5",
			},
		},
		{
			Name: "Destroy a redis service named my-redis",
			ServiceCreateTask: ServiceCreateTask{
				Service: "redis",
				Name:    "my-redis",
				State:   "absent",
			},
		},
	})
}

// Execute creates or destroys a dokku service
func (t ServiceCreateTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// setCreateOptions returns the recipe keys of every create-time option the
// task supplied, in field order. Keep it in step with createFlags: an option
// that renders a flag but is missing here would be silently accepted under
// state 'absent', where dokku has nowhere to put it.
func (t ServiceCreateTask) setCreateOptions() []string {
	var set []string
	for _, opt := range []struct {
		name string
		used bool
	}{
		{"image", t.Image != ""},
		{"image_version", t.ImageVersion != ""},
		{"config_options", t.ConfigOptions != ""},
		{"custom_env", len(t.CustomEnv) > 0},
		{"memory", t.Memory != 0},
		{"shm_size", t.ShmSize != ""},
		{"initial_network", t.InitialNetwork != ""},
		{"post_create_network", len(t.PostCreateNetwork) > 0},
		{"post_start_network", len(t.PostStartNetwork) > 0},
		{"password", t.Password != ""},
		{"root_password", t.RootPassword != ""},
	} {
		if opt.used {
			set = append(set, opt.name)
		}
	}
	return set
}

// createFlags renders the create-time options as the flags every datastore
// plugin accepts on `<service>:create`. The order is fixed so plan and apply
// build byte-identical argv across runs.
func (t ServiceCreateTask) createFlags() []string {
	var args []string
	if t.Image != "" {
		args = append(args, "--image", t.Image)
	}
	if t.ImageVersion != "" {
		args = append(args, "--image-version", t.ImageVersion)
	}
	if t.ConfigOptions != "" {
		args = append(args, "--config-options", t.ConfigOptions)
	}
	if env := formatCustomEnv(t.CustomEnv); env != "" {
		args = append(args, "--custom-env", env)
	}
	if t.Memory != 0 {
		args = append(args, "--memory", strconv.Itoa(t.Memory))
	}
	if t.ShmSize != "" {
		args = append(args, "--shm-size", t.ShmSize)
	}
	if t.InitialNetwork != "" {
		args = append(args, "--initial-network", t.InitialNetwork)
	}
	if len(t.PostCreateNetwork) > 0 {
		args = append(args, "--post-create-network", strings.Join(t.PostCreateNetwork, ","))
	}
	if len(t.PostStartNetwork) > 0 {
		args = append(args, "--post-start-network", strings.Join(t.PostStartNetwork, ","))
	}
	if t.Password != "" {
		args = append(args, "--password", t.Password)
	}
	if t.RootPassword != "" {
		args = append(args, "--root-password", t.RootPassword)
	}
	return args
}

// formatCustomEnv renders the custom_env map as the single semicolon-delimited
// `KEY=value` token `--custom-env` expects. Keys are sorted so the rendered
// command is stable across runs.
func formatCustomEnv(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := mapKeys(env)
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, env[k]))
	}
	return strings.Join(pairs, ";")
}

// imageSuffix renders the pinned image for the mutation line. Text `docket
// plan` prints Mutations and never Commands, so without this the pin a recipe
// asked for would not show up in a plan at all. The remaining create-time
// options stay out: two of them are secrets, and the resolved Commands already
// carry the full argv with sensitive values masked.
func (t ServiceCreateTask) imageSuffix() string {
	switch {
	case t.Image != "" && t.ImageVersion != "":
		return fmt.Sprintf(" (image %s:%s)", t.Image, t.ImageVersion)
	case t.Image != "":
		return fmt.Sprintf(" (image %s)", t.Image)
	case t.ImageVersion != "":
		return fmt.Sprintf(" (image version %s)", t.ImageVersion)
	}
	return ""
}

// Validate checks the ServiceCreateTask's inputs without contacting the server.
func (t ServiceCreateTask) Validate() error {
	// `<service>:destroy` takes none of the create-time options, so any
	// supplied alongside state 'absent' would be silently discarded rather
	// than applied. Every other state consumes them.
	if t.State == StateAbsent {
		if set := t.setCreateOptions(); len(set) > 0 {
			return fmt.Errorf("'%s' must not be set for state 'absent'", strings.Join(set, "', '"))
		}
		return nil
	}

	if t.Memory < 0 {
		return fmt.Errorf("'memory' must not be negative")
	}

	// dokku receives custom_env as one `KEY=value;KEY=value` token, so a key
	// or value carrying a delimiter would silently restructure the map.
	keys := mapKeys(t.CustomEnv)
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			return fmt.Errorf("'custom_env' keys must not be empty")
		}
		if strings.ContainsAny(k, "=;") {
			return fmt.Errorf("'custom_env' key %q must not contain '=' or ';'", k)
		}
		if strings.Contains(t.CustomEnv[k], ";") {
			return fmt.Errorf("'custom_env' value for %q must not contain ';'", k)
		}
	}

	// The network lists are joined with commas for the same reason.
	for _, list := range []struct {
		name     string
		networks []string
	}{
		{"post_create_network", t.PostCreateNetwork},
		{"post_start_network", t.PostStartNetwork},
	} {
		for _, network := range list.networks {
			if network == "" {
				return fmt.Errorf("'%s' entries must not be empty", list.name)
			}
			if strings.Contains(network, ",") {
				return fmt.Errorf("'%s' entry %q must not contain ','", list.name, network)
			}
		}
	}
	return nil
}

// Plan reports the drift the ServiceCreateTask would produce.
func (t ServiceCreateTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := append([]string{"--quiet", fmt.Sprintf("%s:create", t.Service), t.Name}, t.createFlags()...)
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    args,
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("%s service %s missing", t.Service, t.Name),
				Mutations: []string{fmt.Sprintf("%s:create %s%s", t.Service, t.Name, t.imageSuffix())},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "--force", fmt.Sprintf("%s:destroy", t.Service), t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%s service %s present", t.Service, t.Name),
				Mutations: []string{fmt.Sprintf("%s:destroy %s", t.Service, t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// serviceExists checks if a dokku service exists. Returns (false, err)
// when the probe could not run - a transport failure, a missing dokku
// binary, or a cancellation, (false, nil) when dokku reports the service
// absent, (true, nil) when present.
func serviceExists(service, name string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:exists", service),
			name,
		},
	})
}

// destroyService destroys a dokku service
func destroyService(service, name string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "present",
	}
	exists, _ := serviceExists(service, name)
	if !exists {
		state.State = "absent"
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"--force",
			fmt.Sprintf("%s:destroy", service),
			name,
		},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state = state.WithExecResult(result)
	state.Changed = true
	state.State = "absent"
	return state
}

// init registers the ServiceCreateTask with the task registry
func init() {
	RegisterTask(&ServiceCreateTask{})
}
