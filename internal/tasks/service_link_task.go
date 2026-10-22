package tasks

import (
	"context"
	"fmt"
	"github.com/dokku/docket/internal/subprocess"
)

// ServiceLinkTask links or unlinks a dokku service to an app
type ServiceLinkTask struct {
	// App is the name of the app to link the service to
	App string `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app to link the service to"`

	// Service is the type of service to link (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" serialize:"service.type" yaml:"service" description:"Type of service to link (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" serialize:"service.name" yaml:"name" description:"Name of the service instance"`

	// State is the desired state of the service link
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the service link"`
}

// ServiceLinkTaskExample contains an example of a ServiceLinkTask
type ServiceLinkTaskExample struct {
	// Name is the task name holding the ServiceLinkTask description
	Name string `yaml:"-"`

	// ServiceLinkTask is the ServiceLinkTask configuration
	ServiceLinkTask ServiceLinkTask `yaml:"dokku_service_link"`
}

// GetName returns the name of the example
func (e ServiceLinkTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service link task
func (t ServiceLinkTask) Doc() string {
	return "Links or unlinks a dokku service to an app"
}

// ExportSupport reports how docket export handles this task.
func (t ServiceLinkTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ServiceLinkTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// 2.0.0 is the datastore generation whose `<service>:linked` exit status
// this task parses. It is not the 2.3.1 fingerprint floor the info readers
// use; link does not read those keys. 0.38.31 is the Dokku the integration
// suite runs.
func (t ServiceLinkTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			NameField: "Service",
			Minimum:   "2.0.0",
			Text:      "a dokku datastore service plugin >= 2.0.0 matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)",
		}},
	}
}

// ExportApp reconstructs an app's datastore service links: it enumerates
// services (listServices) and emits a dokku_service_link for each service the
// app appears in (serviceLinkedApps). The link, not dokku_config, is the source
// of truth for the injected `<ALIAS>_URL`, so config export drops those values
// (see linkedServiceDSNs).
func (t ServiceLinkTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		apps, err := serviceLinkedApps(ctx, s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if !apps[app] {
			continue
		}
		out = append(out, ServiceLinkTask{App: app, Service: s.Type, Name: s.Name, State: StatePresent})
	}
	return out, nil
}

// examples returns a list of ServiceLinkTaskExamples as yaml
func (t ServiceLinkTask) examples() ([]Doc, error) {
	return MarshalExamples([]ServiceLinkTaskExample{
		{
			Name: "Link a redis service named my-redis to my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "redis",
				Name:    "my-redis",
			},
		},
		{
			Name: "Link a postgres service named my-db to my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "postgres",
				Name:    "my-db",
			},
		},
		{
			Name: "Unlink a redis service named my-redis from my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "redis",
				Name:    "my-redis",
				State:   "absent",
			},
		},
	})
}

// Execute links or unlinks a dokku service to an app
func (t ServiceLinkTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// ObjectEffects reports that the task restarts the app: <service>:link and
// <service>:unlink restart it unless passed --no-restart, which the task never
// passes.
func (t ServiceLinkTask) ObjectEffects() ObjectEffects {
	return ObjectEffects{
		Restarts: []ObjectRestart{{Kind: ObjectApp}},
	}
}

// Plan reports the drift the ServiceLinkTask would produce.
func (t ServiceLinkTask) Plan(ctx context.Context) (result PlanResult) {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	defer recordPlanned(&result, t)
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			svcExists, err := serviceExists(ctx, t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !svcExists {
				return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectService)}
			}
			appOK, err := appExists(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !appOK {
				return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectApp)}
			}
			linked, err := serviceLinked(ctx, t.Service, t.Name, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if linked {
				return observedServiceLink(t, PlanResult{InSync: true, Status: PlanStatusOK}, linked)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:link", t.Service), t.Name, t.App},
			}}
			return observedServiceLink(t, PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("%s service %s not linked to %s", t.Service, t.Name, t.App),
				Mutations: []string{fmt.Sprintf("%s:link %s %s", t.Service, t.Name, t.App)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}, linked)
		},
		StateAbsent: func() PlanResult {
			svcExists, err := serviceExists(ctx, t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !svcExists {
				return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectService)}
			}
			appOK, err := appExists(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !appOK {
				return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectApp)}
			}
			linked, err := serviceLinked(ctx, t.Service, t.Name, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !linked {
				return observedServiceLink(t, PlanResult{InSync: true, Status: PlanStatusOK}, linked)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:unlink", t.Service), t.Name, t.App},
			}}
			return observedServiceLink(t, PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%s service %s linked to %s", t.Service, t.Name, t.App),
				Mutations: []string{fmt.Sprintf("%s:unlink %s %s", t.Service, t.Name, t.App)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}, linked)
		},
	})
}

// serviceLinked checks if a dokku service is linked to an app. Returns
// (false, err) when the probe could not run - a transport failure, a
// missing dokku binary, or a cancellation; (false, nil) when dokku
// reports no link; (true, nil) when linked.
func serviceLinked(ctx context.Context, service, name, app string) (bool, error) {
	return subprocess.Probe(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:linked", service),
			name,
			app,
		},
	})
}

// init registers the ServiceLinkTask with the task registry
func init() {
	RegisterTask(&ServiceLinkTask{})
}

// observedServiceLink attaches the link the probe read to r: the task's identity,
// present when the service is linked to the app.
func observedServiceLink(t ServiceLinkTask, r PlanResult, linked bool) PlanResult {
	o := ServiceLinkTask{App: t.App, Service: t.Service, Name: t.Name, State: StateAbsent}
	if linked {
		o.State = StatePresent
	}
	return observed(r, t, o, linked)
}
