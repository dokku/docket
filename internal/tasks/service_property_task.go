package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// ServicePropertyTask manages a single dokku service property via
// <service>:set.
//
// The property is read back from `<service>:info --format json`, which reports
// every key `<service>:set` accepts (dokku-datastore 0.7.1+), so the task
// converges. Most keys are reported under their own name and as they were
// set; servicePropertyReadbacks covers the rest: keys reported with a default
// standing in for an unset value, and the backup credentials, which are
// reported only as a sha256 fingerprint of the stored value.
//
// The property names are not validated here. They come from whichever
// datastore plugin backs the service, and some are refused for a particular
// datastore - the backup settings on one that cannot be backed up, extra
// export or import arguments where the definition takes none. `<service>:set`
// enforces both, so a refused key plans as drift and the apply fails with the
// plugin's own error.
type ServicePropertyTask struct {
	// Service is the type of service to configure (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" yaml:"service" description:"Type of service to configure (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the service instance"`

	// Property is the name of the property to set
	Property string `required:"true" identity:"key" yaml:"property" description:"Name of the property to set. Which properties a service accepts depends on its datastore - the backup settings only on one that can be backed up, for example - and <service>:set refuses the rest when the task applies."`

	// Value is the value to set the property to. Required when state is present.
	Value string `required:"false" required_when:"present" forbidden_when:"absent" yaml:"value,omitempty" description:"Value to set the property to. Masked in output for the backup credentials and custom-env."`

	// State is the desired state of the property
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the property"`
}

// servicePropertyReadback says how a property is read back when info does not
// simply report it under its own name, as it was set.
type servicePropertyReadback struct {
	// infoKey is the info key that reports the property, when it is not the
	// property's own name.
	infoKey string

	// unset is what info reports for the property when it was never set or
	// was cleared, when that is not the empty string.
	unset string

	// fingerprint marks a credential info reports only as the sha256 of the
	// stored value, empty when nothing is stored. The desired value is
	// compared by its own digest, and neither is ever printed.
	fingerprint bool

	// env marks custom-env, compared as the map it encodes so pair order is
	// not drift.
	env bool

	// sensitive marks a value that is masked in output.
	sensitive bool
}

// servicePropertyReadbacks are the properties info reports other than under
// their own name and as they were set. Each one mirrors how dokku-datastore's
// Info renders it.
var servicePropertyReadbacks = map[string]servicePropertyReadback{
	"expose-mode":                  {unset: "ambassador"},
	"backup-timestamp":             {unset: "true"},
	"backup-use-iam":               {unset: "false"},
	"memory":                       {unset: "0"},
	"custom-env":                   {env: true, sensitive: true},
	"backup-access-key-id":         {infoKey: "backup-access-key-id-fingerprint", fingerprint: true, sensitive: true},
	"backup-secret-access-key":     {infoKey: "backup-secret-access-key-fingerprint", fingerprint: true, sensitive: true},
	"backup-encryption-passphrase": {infoKey: "backup-encryption-fingerprint", fingerprint: true, sensitive: true},
}

// servicePropertyExportKeys are the properties docket export reads back, in
// emission order. Keys another task owns are left out so a service's state is
// exported once: the networks, config-options, custom-env, memory and shm-size
// belong to dokku_service_create, and the schedule, bucket, IAM flag, region,
// signature version, endpoint and credentials to dokku_service_backup.
var servicePropertyExportKeys = []string{
	"backup-keyserver",
	"backup-storage-class",
	"backup-object-name",
	"backup-timestamp",
	"backup-mailto",
	"backup-public-key-id",
	"log-driver",
	"log-opt",
	"restart-policy",
	"wait-timeout",
	"port-bind-address",
	"port-source-range",
	"expose-host",
	"expose-mode",
	"export-args",
	"import-args",
	"volume-targets",
}

// readback returns how the task's property is read back.
func (t ServicePropertyTask) readback() (servicePropertyReadback, string) {
	rb := servicePropertyReadbacks[t.Property]
	key := rb.infoKey
	if key == "" {
		key = t.Property
	}
	return rb, key
}

// ServicePropertyTaskExample contains an example of a ServicePropertyTask
type ServicePropertyTaskExample struct {
	// Name is the task name holding the ServicePropertyTask description
	Name string `yaml:"-"`

	// ServicePropertyTask is the ServicePropertyTask configuration
	ServicePropertyTask ServicePropertyTask `yaml:"dokku_service_property"`
}

// GetName returns the name of the example
func (e ServicePropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service property task
func (t ServicePropertyTask) Doc() string {
	return "Manages a property for a given dokku service"
}

// ExportSupport reports how docket export handles this task.
func (t ServicePropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "every property set on a service is exported except the ones another task owns (the networks, config-options, custom-env, memory and shm-size belong to dokku_service_create; the backup schedule, bucket, IAM flag, region, signature version, endpoint and credentials to dokku_service_backup); the backup credentials are reported only as fingerprints, so they must be re-supplied"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ServicePropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The plugin floor is the datastore release whose `<service>:info` reports
// every key this task reads. 0.38.31 is the Dokku the integration suite runs;
// the probe is `<service>:info`.
func (t ServicePropertyTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			NameField: "Service",
			Minimum:   "2.3.1",
			Text:      datastorePluginRequirement,
		}},
	}
}

// ExportGlobal reconstructs the properties set on every datastore service
// instance, one task per property in servicePropertyExportKeys that holds a
// value other than its unset default. Discovery is via listServices; every
// property of a service comes from one `<service>:info` call. A property a
// datastore refuses always reads back empty, so a datastore without backups,
// extra arguments or moved volumes yields no task for them.
func (t ServicePropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		info, err := serviceInfo(ctx, s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		for _, property := range servicePropertyExportKeys {
			value := strings.TrimSpace(info[property])
			if value == "" || value == servicePropertyReadbacks[property].unset {
				continue
			}
			out = append(out, ServicePropertyTask{
				Service:  s.Type,
				Name:     s.Name,
				Property: property,
				Value:    value,
				State:    StatePresent,
			})
		}
	}
	return out, nil
}

// SensitiveValues masks the value of a property that holds a secret: the
// backup credentials and custom-env. The struct tag cannot say this, since
// whether Value is a secret depends on which property it is.
func (t ServicePropertyTask) SensitiveValues() []string {
	rb, _ := t.readback()
	if !rb.sensitive || t.Value == "" {
		return nil
	}
	values := []string{t.Value}
	if rb.env {
		for _, v := range parseCustomEnv(t.Value) {
			values = append(values, v)
		}
	}
	return values
}

// examples returns a list of ServicePropertyTaskExamples as yaml
func (t ServicePropertyTask) examples() ([]Doc, error) {
	return MarshalExamples([]ServicePropertyTaskExample{
		{
			Name: "Set the restart-policy for a postgres service",
			ServicePropertyTask: ServicePropertyTask{
				Service:  "postgres",
				Name:     "my-db",
				Property: "restart-policy",
				Value:    "always",
			},
		},
		{
			Name: "Publish an exposed postgres service directly rather than through an ambassador",
			ServicePropertyTask: ServicePropertyTask{
				Service:  "postgres",
				Name:     "my-db",
				Property: "expose-mode",
				Value:    "direct",
			},
		},
		{
			Name: "Clear the restart-policy for a postgres service",
			ServicePropertyTask: ServicePropertyTask{
				Service:  "postgres",
				Name:     "my-db",
				Property: "restart-policy",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or clears the dokku service property
func (t ServicePropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the ServicePropertyTask's inputs without contacting the server.
func (t ServicePropertyTask) Validate() error {
	if err := checkConstraints(t); err != nil {
		return err
	}
	if t.Property == "" {
		return fmt.Errorf("'property' is required")
	}
	return nil
}

// Plan reports the drift the ServicePropertyTask would produce, reading the
// property's current value from `<service>:info` once the service is
// confirmed to exist.
func (t ServicePropertyTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	rb, key := t.readback()
	target := fmt.Sprintf("%s service %s", t.Service, t.Name)
	if rb.sensitive {
		subprocess.MaskerFromContext(ctx).Add(t.SensitiveValues()...)
	}

	// probe reads the property's current value. ok is false when the payload
	// has no key for it, which leaves the task unable to compare: the warning
	// says so and the set or clear is applied anyway.
	probe := func() (current string, ok bool, warnings []PlanWarning, result *PlanResult) {
		exists, err := serviceExists(ctx, t.Service, t.Name)
		if err != nil {
			return "", false, nil, &PlanResult{Status: PlanStatusError, Error: err}
		}
		if !exists {
			return "", false, nil, &PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
		}
		info, err := serviceInfo(ctx, t.Service, t.Name)
		if err != nil {
			return "", false, nil, &PlanResult{Status: PlanStatusError, Error: err}
		}
		value, ok := info[key]
		if !ok {
			return "", false, []PlanWarning{{
				Reason:  WarnReasonUnknownProperty,
				Message: fmt.Sprintf("dokku %s:info has no key %q for property %q, so it was not compared", t.Service, key, t.Property),
			}}, nil
		}
		value = strings.TrimSpace(value)
		if rb.env {
			registerSensitiveMapValues(ctx, parseCustomEnv(value))
			subprocess.MaskerFromContext(ctx).Add(value)
		}
		return value, true, nil, nil
	}

	// seen attaches the property as info reported it. A value equal to the
	// property's unset default is no value, as export reads it. A fingerprinted
	// credential is reported only as a digest, so its value is known only when
	// the digest matches the declared one; otherwise Observed carries the
	// property as set with no value. A property info does not report leaves
	// Observed nil.
	seen := func(r PlanResult, current string, ok bool) PlanResult {
		if !ok {
			return r
		}
		o := ServicePropertyTask{Service: t.Service, Name: t.Name, Property: t.Property, State: StateAbsent}
		if current == "" || (rb.unset != "" && current == rb.unset && !rb.fingerprint) {
			return observed(r, o, false)
		}
		o.State = StatePresent
		switch {
		case !rb.fingerprint:
			o.Value = current
		case servicePropertyMatches(rb, current, t.Value):
			o.Value = t.Value
		}
		return observed(r, o, true)
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			current, ok, warnings, result := probe()
			if result != nil {
				return *result
			}
			status := PlanStatusModify
			reason := fmt.Sprintf("would set %s on %s (not reported by info)", t.Property, target)
			if ok {
				if servicePropertyMatches(rb, current, t.Value) {
					return seen(PlanResult{InSync: true, Status: PlanStatusOK}, current, ok)
				}
				switch {
				case current == "" || (rb.unset != "" && current == rb.unset && !rb.fingerprint):
					status = PlanStatusCreate
					reason = fmt.Sprintf("%s missing on %s", t.Property, target)
				case rb.fingerprint:
					reason = fmt.Sprintf("%s on %s differs from the stored value", t.Property, target)
				case rb.env:
					reason = fmt.Sprintf("%s on %s differs for %s", t.Property, target,
						strings.Join(customEnvDiff(parseCustomEnv(current), parseCustomEnv(t.Value)), ", "))
				default:
					reason = fmt.Sprintf("%s drift on %s (was %q)", t.Property, target, current)
				}
			}
			mutation := fmt.Sprintf("%s:set %s %s %s", t.Service, t.Name, t.Property, t.Value)
			if rb.sensitive {
				mutation = fmt.Sprintf("%s:set %s %s", t.Service, t.Name, t.Property)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:set", t.Service), t.Name, t.Property, t.Value},
			}}
			return seen(PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    reason,
				Mutations: []string{mutation},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}, current, ok)
		},
		StateAbsent: func() PlanResult {
			current, ok, warnings, result := probe()
			if result != nil {
				return *result
			}
			reason := fmt.Sprintf("would unset %s on %s (not reported by info)", t.Property, target)
			if ok {
				if current == "" || (rb.unset != "" && current == rb.unset) {
					return seen(PlanResult{InSync: true, Status: PlanStatusOK}, current, ok)
				}
				reason = fmt.Sprintf("would unset %s on %s (was %q)", t.Property, target, current)
				if rb.sensitive {
					reason = fmt.Sprintf("would unset %s on %s", t.Property, target)
				}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:set", t.Service), t.Name, t.Property},
			}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("%s:set %s %s (clear)", t.Service, t.Name, t.Property)},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}, current, ok)
		},
	})
}

// servicePropertyMatches reports whether the value info reports for a
// property is the one the recipe declares, after the property's readback
// rule: a fingerprint is compared against the declared value's digest, and
// custom-env as the map it encodes.
func servicePropertyMatches(rb servicePropertyReadback, current, desired string) bool {
	switch {
	case rb.fingerprint:
		return current != "" && current == backupFingerprint(desired)
	case rb.env:
		return len(customEnvDiff(parseCustomEnv(current), parseCustomEnv(desired))) == 0
	case current == "" && rb.unset != "":
		return desired == rb.unset
	}
	return current == desired
}

// init registers the ServicePropertyTask with the task registry
func init() {
	RegisterTask(&ServicePropertyTask{})
}
