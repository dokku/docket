package tasks

import (
	"context"
	"fmt"
	"slices"
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
// They apply when the service is created. Once it exists dokku's only remedy
// is `<service>:upgrade`, which removes and recreates the container, so the
// task does not reach for it on its own: `image_drift` decides what happens
// when the recipe's `image` / `image_version` pin no longer matches what the
// service is running, and it defaults to reporting the mismatch rather than
// acting on it. The remaining create-time options have no read command at all
// (see ProbeSupport), so they are neither compared nor reconciled.
type ServiceCreateTask struct {
	// Service is the type of service to create (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" yaml:"service" description:"Type of service to create (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the service instance"`

	// Image is the image the service container is started from.
	Image string `required:"false" yaml:"image,omitempty" description:"Image to start the service with, e.g. postgis/postgis. Applied when the service is created; image_drift decides what happens when an existing service is running something else."`

	// ImageVersion is the tag of the image the service container is started from.
	ImageVersion string `required:"false" yaml:"image_version,omitempty" description:"Image tag to start the service with. Applied when the service is created; image_drift decides what happens when an existing service is running something else."`

	// ConfigOptions are extra arguments passed to the container create command.
	ConfigOptions string `required:"false" yaml:"config_options,omitempty" description:"Extra arguments to pass to the container create command. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// CustomEnv is the environment the service container is started with.
	CustomEnv map[string]string `required:"false" sensitive:"true" yaml:"custom_env,omitempty" description:"Map of environment variables to start the service with. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// Memory is the container memory limit in megabytes.
	Memory int `required:"false" yaml:"memory,omitempty" description:"Container memory limit in megabytes. Applied only when the service is created."`

	// ShmSize is the shared memory size of the service container.
	ShmSize string `required:"false" yaml:"shm_size,omitempty" description:"Shared memory size for the service container. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// InitialNetwork is the network the service is attached to on creation.
	InitialNetwork string `required:"false" yaml:"initial_network,omitempty" description:"Network to attach the service to initially. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// PostCreateNetwork are the networks attached after service creation.
	PostCreateNetwork []string `required:"false" yaml:"post_create_network,omitempty" description:"Networks to attach the service container to after creation. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// PostStartNetwork are the networks attached after service start.
	PostStartNetwork []string `required:"false" yaml:"post_start_network,omitempty" description:"Networks to attach the service container to after start. Applied when the service is created, and re-applied when image_drift 'upgrade' recreates it."`

	// Password overrides the user-level service password.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Override the user-level service password. Applied only when the service is created."`

	// RootPassword overrides the root-level service password.
	RootPassword string `required:"false" sensitive:"true" yaml:"root_password,omitempty" description:"Override the root-level service password. Applied only when the service is created."`

	// ImageDrift is what the task does when the service already exists on an
	// image other than the one `image` / `image_version` pins. Read through
	// imageDriftMode, never directly: the `default:` tag is only applied to a
	// decoded recipe, so a task built in Go carries the empty string.
	ImageDrift string `required:"false" yaml:"image_drift,omitempty" default:"warn" options:"ignore,warn,error,upgrade" description:"What to do when the service already exists on an image other than the one pinned: 'ignore' it, 'warn' and leave it alone, 'error' out, or 'upgrade' the service to reconcile it. Upgrading recreates the container, which means downtime, no data migration across major versions, and it clears any config_options and custom_env the recipe does not declare."`

	// RestartApps forces the apps linked to the service to restart after an
	// image_drift upgrade has recreated its container.
	RestartApps bool `required:"false" yaml:"restart_apps,omitempty" description:"Restart the apps linked to the service after an image_drift 'upgrade' recreates its container, so they reconnect to the new one. Only valid with image_drift 'upgrade', and only acted on when the service has linked apps."`

	// State is the desired state of the service
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the service"`
}

// The image_drift modes: what the task does when the service exists on an
// image other than the one the recipe pins. `warn` is the default because
// dokku's only remedy recreates the container, which is not something to do
// to a datastore on the strength of a field moving in a recipe.
const (
	imageDriftIgnore  = "ignore"
	imageDriftWarn    = "warn"
	imageDriftError   = "error"
	imageDriftUpgrade = "upgrade"
)

// imageDriftModes is the accepted set, in the order Validate names them and
// the `options:` tag publishes them.
var imageDriftModes = []string{imageDriftIgnore, imageDriftWarn, imageDriftError, imageDriftUpgrade}

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
	return ProbeSupport{Status: ProbePartial, Caveat: "the service's existence is probed, and the image it is running whenever the recipe pins one; config_options, custom_env, memory, shm_size, the networks, and the passwords have no read command and are not drift-detected"}
}

// ExportGlobal reconstructs every datastore service instance on the server as a
// dokku_service_create task. Discovery is via listServices (the `service-list`
// plugin trigger). Like dokku_storage_mount, the service's data is migrated
// separately - but the image it runs is exported, so a service recreated on
// another server comes up on the same version rather than whatever that
// server's plugin defaults to, which is what its `:import` counterpart needs.
func (t ServiceCreateTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		image, version, err := serviceImage(ctx, s.Type, s.Name)
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
			Name: "Reconcile a changed image pin by upgrading the service",
			ServiceCreateTask: ServiceCreateTask{
				Service:      "redis",
				Name:         "my-upgraded-redis",
				Image:        "redis",
				ImageVersion: "7.2.5",
				ImageDrift:   imageDriftUpgrade,
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
func (t ServiceCreateTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// setCreateOptions returns the recipe keys of every create-time option the
// task supplied, in field order. Keep it in step with createFlags: an option
// that renders a flag but is missing here would be silently accepted under
// state 'absent', where dokku has nowhere to put it.
//
// image_drift and restart_apps are deliberately absent: they render no flag on
// any subcommand, so createFlags is not the list they belong to. Validate
// rejects them under state 'absent' on their own terms.
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

// imageDriftMode returns the drift policy in force. The `default:` tag is
// applied by defaults.SetDefaults when a recipe is decoded, but a task built
// in Go - by ExportGlobal, by an integration test, by Examples - never passes
// through that, so the zero value has to mean the same thing as the default.
func (t ServiceCreateTask) imageDriftMode() string {
	if t.ImageDrift == "" {
		return imageDriftWarn
	}
	return t.ImageDrift
}

// pinsImage reports whether the recipe named any part of the image the
// service should run. Nothing is compared when it did not: the plugin's own
// default is then the declared value, and docket cannot know what it is.
func (t ServiceCreateTask) pinsImage() bool {
	return t.Image != "" || t.ImageVersion != ""
}

// upgradeFlags renders the create-time options as the flags
// `<service>:upgrade` accepts. Everything the recipe declares is re-passed
// rather than left off, because the plugin's service_commit_config rewrites
// CONFIG_OPTIONS and ENV from the flags it was given and blanks them when it
// was given none - so an upgrade that omits them is a silent reset. memory,
// password, and root_password are absent because `:upgrade` does not accept
// them; the plugin keeps its own copies of those and reapplies them when it
// rebuilds the container.
func (t ServiceCreateTask) upgradeFlags(image string) []string {
	args := []string{"--image", image, "--image-version", t.ImageVersion}
	if t.ConfigOptions != "" {
		args = append(args, "--config-options", t.ConfigOptions)
	}
	if env := formatCustomEnv(t.CustomEnv); env != "" {
		args = append(args, "--custom-env", env)
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
	return args
}

// upgradeResets names the create-time options `<service>:upgrade` will blank
// because the recipe does not declare them and docket has no read command to
// carry the server's values across. It is the one destructive thing an
// upgrade does that nothing else in the plan would show: an option docket
// never learned about renders as no flag at all, and no flag is what makes
// the plugin write an empty value. Plan surfaces it as its own mutation line,
// since text `docket plan` prints Mutations and never Commands.
func (t ServiceCreateTask) upgradeResets() []string {
	var reset []string
	if t.ConfigOptions == "" {
		reset = append(reset, "config_options")
	}
	if len(t.CustomEnv) == 0 {
		reset = append(reset, "custom_env")
	}
	return reset
}

// pinnedDescription renders what the recipe asked the service to run, for the
// human half of a drift report. A recipe that pins only the version is
// describing the running image's name with a different tag, so the running
// name is filled in; one that pins only the name has not said anything about
// the tag, and the phrasing says so rather than inventing one.
func (t ServiceCreateTask) pinnedDescription(runningImage string) string {
	switch {
	case t.Image != "" && t.ImageVersion != "":
		return fmt.Sprintf("%s:%s", t.Image, t.ImageVersion)
	case t.ImageVersion != "" && runningImage != "":
		return fmt.Sprintf("%s:%s", runningImage, t.ImageVersion)
	case t.ImageVersion != "":
		return fmt.Sprintf("image version %s", t.ImageVersion)
	default:
		return fmt.Sprintf("image %s", t.Image)
	}
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
		// Neither drift field renders a flag, so setCreateOptions does not
		// carry them. A service being destroyed has no pin to drift from, so
		// either one is a mistake worth naming rather than ignoring.
		if t.imageDriftMode() != imageDriftWarn {
			return fmt.Errorf("'image_drift' must not be set for state 'absent'")
		}
		if t.RestartApps {
			return fmt.Errorf("'restart_apps' must not be set for state 'absent'")
		}
		return nil
	}

	mode := t.imageDriftMode()
	if !slices.Contains(imageDriftModes, mode) {
		return fmt.Errorf("'image_drift' must be one of %s, got %q", strings.Join(imageDriftModes, ", "), t.ImageDrift)
	}
	// Every mode but the default acts on a comparison, and there is nothing to
	// compare against when the recipe has not said what the service should run.
	if mode != imageDriftWarn && !t.pinsImage() {
		return fmt.Errorf("'image_drift' requires 'image' or 'image_version'")
	}
	// `<service>:upgrade` needs both halves of the reference and falls back to
	// the plugin's default for either one it is not given. docket can fill a
	// missing name from the running container, because a recipe that pins only
	// the version is asking for the same image on a new tag. It cannot fill a
	// missing tag the same way: the running tag belongs to the image being
	// replaced, and carrying it over would name a reference that may not exist
	// - discovered only after the old container is gone.
	if mode == imageDriftUpgrade && t.Image != "" && t.ImageVersion == "" {
		return fmt.Errorf("'image_drift: upgrade' requires 'image_version' when 'image' is set")
	}
	if t.RestartApps && mode != imageDriftUpgrade {
		return fmt.Errorf("'restart_apps' requires 'image_drift: upgrade'")
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
func (t ServiceCreateTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := serviceExists(ctx, t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				if drift := planServiceImageDrift(ctx, t); drift != nil {
					return *drift
				}
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
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := serviceExists(ctx, t.Service, t.Name)
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
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// planServiceImageDrift reports what an existing service's image means for the
// task, or nil when it means nothing and the caller should carry on to its own
// in-sync result. nil covers three cases: the recipe pinned no image, the
// recipe asked for the comparison to be skipped, and the running image already
// matches.
//
// It is a package-level func rather than a method because
// TestProbeSupportMatchesPlanWiring walks the tasks package for plan-returning
// methods and allows exactly one per task, Plan itself.
func planServiceImageDrift(ctx context.Context, t ServiceCreateTask) *PlanResult {
	mode := t.imageDriftMode()
	if mode == imageDriftIgnore || !t.pinsImage() {
		return nil
	}

	ref, err := serviceImageRef(ctx, t.Service, t.Name)
	if err != nil {
		return &PlanResult{Status: PlanStatusError, Error: err}
	}

	running := strings.TrimSpace(ref)
	if running == "" {
		// dokku prints nothing here when the service's container is gone, and
		// an older plugin may not answer `--version` at all. Either way the
		// comparison did not happen, and a recipe that asked for drift to be
		// caught should not be told everything is fine.
		return &PlanResult{
			InSync: true,
			Status: PlanStatusOK,
			Warnings: []PlanWarning{{
				Reason: WarnReasonServiceImageDrift,
				Message: fmt.Sprintf("%s service %s reports no running image, so the recipe's pin of %s was not checked",
					t.Service, t.Name, t.pinnedDescription("")),
			}},
		}
	}

	image, version := splitImageRef(running)
	if (t.Image == "" || t.Image == image) && (t.ImageVersion == "" || t.ImageVersion == version) {
		return nil
	}

	switch mode {
	case imageDriftError:
		return &PlanResult{
			Status: PlanStatusError,
			Error: fmt.Errorf("%s service %s is running %s, recipe pins %s",
				t.Service, t.Name, running, t.pinnedDescription(image)),
		}
	case imageDriftUpgrade:
		return planServiceImageUpgrade(ctx, t, running, image)
	}

	return &PlanResult{
		InSync: true,
		Status: PlanStatusOK,
		Warnings: []PlanWarning{{
			Reason: WarnReasonServiceImageDrift,
			Message: fmt.Sprintf("%s service %s is running %s, recipe pins %s; create-time options are not reconciled, set image_drift: upgrade to run %s:upgrade",
				t.Service, t.Name, running, t.pinnedDescription(image), t.Service),
		}},
	}
}

// planServiceImageUpgrade builds the `<service>:upgrade` that reconciles the
// pin. running is the reference the container reports and image is its name
// half; Validate has already established that the recipe pins image_version,
// so the only half that may need filling is the name.
//
// Package-level for the same reason as planServiceImageDrift.
func planServiceImageUpgrade(ctx context.Context, t ServiceCreateTask, running, image string) *PlanResult {
	if t.Image != "" {
		image = t.Image
	} else if image == "" || strings.Contains(running, "@") {
		// A recipe pinning only the version needs the running name to build a
		// reference. An untagged image gives one, but a digest reference does
		// not: splitImageRef divides it on the digest's own colon, and half of
		// a digest is not a name to hand back to dokku.
		return &PlanResult{
			Status: PlanStatusError,
			Error: fmt.Errorf("cannot upgrade %s service %s: its running image %q gives no image name to keep, set 'image' alongside 'image_version'",
				t.Service, t.Name, running),
		}
	}
	target := fmt.Sprintf("%s:%s", image, t.ImageVersion)

	restartApps := false
	if t.RestartApps {
		apps, err := serviceLinkedApps(ctx, t.Service, t.Name)
		if err != nil {
			return &PlanResult{Status: PlanStatusError, Error: err}
		}
		restartApps = len(apps) > 0
	}

	args := append([]string{"--quiet", fmt.Sprintf("%s:upgrade", t.Service), t.Name}, t.upgradeFlags(image)...)
	if restartApps {
		args = append(args, "--restart-apps", "true")
	}
	inputs := []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    args,
	}}

	mutations := []string{fmt.Sprintf("%s:upgrade %s (image %s)", t.Service, t.Name, target)}
	if reset := t.upgradeResets(); len(reset) > 0 {
		mutations = append(mutations, fmt.Sprintf("reset %s, which the recipe does not declare", strings.Join(reset, " and ")))
	}
	if restartApps {
		mutations = append(mutations, "restart the linked app(s)")
	}

	return &PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("image drift: %s -> %s", running, target),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StatePresent, inputs)
		},
	}
}

// serviceExists checks if a dokku service exists. Returns (false, err)
// when the probe could not run - a transport failure, a missing dokku
// binary, or a cancellation, (false, nil) when dokku reports the service
// absent, (true, nil) when present.
func serviceExists(ctx context.Context, service, name string) (bool, error) {
	return subprocess.Probe(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:exists", service),
			name,
		},
	})
}

// destroyService destroys a dokku service
func destroyService(ctx context.Context, service, name string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "present",
	}
	exists, _ := serviceExists(ctx, service, name)
	if !exists {
		state.State = "absent"
		return state
	}

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
