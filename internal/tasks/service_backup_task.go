package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// ServiceBackupTask manages the S3 backup configuration for a dokku
// datastore service: the backup schedule, the S3 authentication, and the
// backup encryption passphrase.
//
// Every piece is read back from `<service>:info --format json`
// (dokku-datastore 0.7.1+). The schedule, bucket, IAM flag, region, signature
// version and endpoint are reported as set; each credential and the
// passphrase are reported only as the sha256 fingerprint of the stored value,
// which is compared against the digest of the recipe's value, so no secret is
// ever read back.
//
// state: absent removes whichever of the schedule, the auth settings and the
// passphrase info reports as stored, without needing the secrets in the
// recipe. The public key, keyserver, mailto, storage class, object name and
// timestamp settings are left alone; they belong to dokku_service_property.
//
// Whether a datastore can be backed up at all is not checked here. On one
// that cannot, info reports nothing configured, so state: absent is in sync,
// and state: present plans as drift and fails with the plugin's own error.
type ServiceBackupTask struct {
	// Service is the type of service to back up (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" yaml:"service" description:"Type of service to back up (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the service instance"`

	// Schedule is the cron schedule for backups. Requires bucket.
	Schedule string `required:"false" yaml:"schedule,omitempty" description:"Cron schedule for backups"`

	// Bucket is the S3 bucket backups are written to. Requires schedule.
	Bucket string `required:"false" yaml:"bucket,omitempty" description:"S3 bucket backups are written to"`

	// UseIam configures the schedule to use the instance IAM profile instead of static credentials.
	UseIam bool `required:"false" yaml:"use_iam,omitempty" description:"Use the instance IAM profile instead of static credentials"`

	// AwsAccessKeyID is the AWS access key id used for backup auth. Requires aws_secret_access_key.
	AwsAccessKeyID string `required:"false" write_only:"true" yaml:"aws_access_key_id,omitempty" description:"AWS access key id for backup auth"`

	// AwsSecretAccessKey is the AWS secret access key used for backup auth. Requires aws_access_key_id.
	AwsSecretAccessKey string `required:"false" sensitive:"true" write_only:"true" yaml:"aws_secret_access_key,omitempty" description:"AWS secret access key for backup auth"`

	// AwsDefaultRegion is the AWS region used for backup auth.
	AwsDefaultRegion string `required:"false" yaml:"aws_default_region,omitempty" description:"AWS region used for backup auth"`

	// AwsSignatureVersion is the AWS signature version used for backup auth.
	AwsSignatureVersion string `required:"false" yaml:"aws_signature_version,omitempty" description:"AWS signature version used for backup auth"`

	// EndpointURL is the S3-compatible endpoint url used for backup auth.
	EndpointURL string `required:"false" yaml:"endpoint_url,omitempty" description:"S3-compatible endpoint url used for backup auth"`

	// EncryptionPassphrase is the passphrase used to encrypt future backups.
	EncryptionPassphrase string `required:"false" sensitive:"true" write_only:"true" yaml:"encryption_passphrase,omitempty" description:"Passphrase used to encrypt future backups"`

	// State is the desired state of the backup configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the backup configuration. 'absent' removes whichever of the schedule, the auth settings and the encryption passphrase the service reports as stored, without needing the secrets in the recipe, and leaves the settings dokku_service_property manages alone. On a datastore that cannot be backed up, 'absent' is always in sync and 'present' fails with the plugin's own error."`
}

// ServiceBackupTaskExample contains an example of a ServiceBackupTask
type ServiceBackupTaskExample struct {
	// Name is the task name holding the ServiceBackupTask description
	Name string `yaml:"-"`

	// ServiceBackupTask is the ServiceBackupTask configuration
	ServiceBackupTask ServiceBackupTask `yaml:"dokku_service_backup"`
}

// GetName returns the name of the example
func (e ServiceBackupTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service backup task
func (t ServiceBackupTask) Doc() string {
	return "Manages the S3 backup schedule, authentication, and encryption for a dokku service"
}

// ExportSupport reports how docket export handles this task.
func (t ServiceBackupTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "the backup schedule, bucket, and use_iam flag are exported; the AWS credentials and encryption passphrase are reported only as fingerprints, and the region, signature version and endpoint cannot be declared without the credentials, so all of these must be re-supplied before the recipe can back up"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ServiceBackupTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The plugin floor is the datastore release whose `<service>:info` reports
// every key this task reads. 0.38.31 is the Dokku the integration suite runs;
// the probe is `<service>:info`.
func (t ServiceBackupTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			NameField: "Service",
			Minimum:   "2.3.1",
			Text:      datastorePluginRequirement,
		}},
	}
}

// ExportGlobal reconstructs the backup schedule of every datastore service on
// the server. Discovery is via listServices; the schedule, bucket, and use_iam
// flag are read from `<service>:info`. The AWS credentials and encryption
// passphrase are reported only as fingerprints, so they are omitted (a
// partial export) and must be re-supplied before the recipe backs up.
// Services without a schedule are skipped, which covers every service of a
// datastore that cannot be backed up.
func (t ServiceBackupTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
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
		schedule := infoValue(info, "backup-schedule")
		bucket := infoValue(info, "backup-bucket")
		if schedule == "" || bucket == "" {
			continue
		}
		out = append(out, ServiceBackupTask{
			Service:  s.Type,
			Name:     s.Name,
			Schedule: schedule,
			Bucket:   bucket,
			UseIam:   infoValue(info, "backup-use-iam") == "true",
			State:    StatePresent,
		})
	}
	return out, nil
}

// examples returns a list of ServiceBackupTaskExamples as yaml
func (t ServiceBackupTask) examples() ([]Doc, error) {
	return MarshalExamples([]ServiceBackupTaskExample{
		{
			Name: "Schedule daily backups of a postgres service to S3",
			ServiceBackupTask: ServiceBackupTask{
				Service:              "postgres",
				Name:                 "my-db",
				Schedule:             "0 3 * * *",
				Bucket:               "my-backup-bucket",
				AwsAccessKeyID:       "AKIAEXAMPLE",
				AwsSecretAccessKey:   "examplesecret",
				AwsDefaultRegion:     "us-east-1",
				EncryptionPassphrase: "correct-horse-battery-staple",
			},
		},
		{
			Name: "Schedule backups using the instance IAM profile",
			ServiceBackupTask: ServiceBackupTask{
				Service:  "postgres",
				Name:     "my-db",
				Schedule: "0 3 * * *",
				Bucket:   "my-backup-bucket",
				UseIam:   true,
			},
		},
		{
			Name: "Remove the backup configuration for a postgres service",
			ServiceBackupTask: ServiceBackupTask{
				Service: "postgres",
				Name:    "my-db",
				State:   StateAbsent,
			},
		},
	})
}

// Execute manages the backup configuration for a dokku service
func (t ServiceBackupTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// hasAuth reports whether any AWS credential field was provided.
func (t ServiceBackupTask) hasAuth() bool {
	return t.AwsAccessKeyID != "" || t.AwsSecretAccessKey != ""
}

// Constraints declares what a present backup configuration needs. A schedule
// writes to a bucket, and backup auth takes both halves of the key pair.
// backup-auth is a fixed positional argv:
//
//	<access-key> <secret> <region> <signature-version> <endpoint>
//
// dokku cannot receive a later positional without every earlier one, and the
// argv builder stops at the first empty field, so each trailing value requires
// the ones before it rather than being silently dropped. And present has to
// configure something.
func (t ServiceBackupTask) Constraints() []Constraint {
	present := []State{StatePresent}
	keys := []string{"aws_access_key_id", "aws_secret_access_key"}
	return []Constraint{
		{Kind: ConstraintAllOrNoneOf, Fields: []string{"schedule", "bucket"}, States: present},
		{Kind: ConstraintAllOrNoneOf, Fields: keys, States: present},
		{Kind: ConstraintRequiredWith, Field: "aws_default_region", Fields: keys, States: present},
		{Kind: ConstraintRequiredWith, Field: "aws_signature_version", Fields: append(append([]string{}, keys...), "aws_default_region"), States: present},
		{Kind: ConstraintRequiredWith, Field: "endpoint_url", Fields: append(append([]string{}, keys...), "aws_signature_version"), States: present},
		{Kind: ConstraintAtLeastOneOf, Fields: []string{"schedule", "aws_access_key_id", "aws_secret_access_key", "encryption_passphrase"}, States: present},
	}
}

// Validate checks the ServiceBackupTask's inputs without contacting the server.
func (t ServiceBackupTask) Validate() error {
	return checkConstraints(t)
}

// Plan reports the drift the ServiceBackupTask would produce, comparing each
// declared group against `<service>:info` once the service is confirmed to
// exist.
func (t ServiceBackupTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	target := fmt.Sprintf("%s service %s", t.Service, t.Name)
	probe := func() (map[string]string, *PlanResult) {
		exists, err := serviceExists(ctx, t.Service, t.Name)
		if err != nil {
			return nil, &PlanResult{Status: PlanStatusError, Error: err}
		}
		if !exists {
			return nil, &PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
		}
		info, err := serviceInfo(ctx, t.Service, t.Name)
		if err != nil {
			return nil, &PlanResult{Status: PlanStatusError, Error: err}
		}
		return info, nil
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			info, result := probe()
			if result != nil {
				return *result
			}

			inputs := []subprocess.ExecCommandInput{}
			mutations := []string{}
			drift := []string{}

			// Keep secrets out of the mutation strings and the reason; the
			// resolved Commands mask registered sensitive values.
			if t.hasAuth() {
				if fields := backupAuthDrift(t, info); len(fields) > 0 {
					authArgs := []string{"--quiet", fmt.Sprintf("%s:backup-auth", t.Service), t.Name, t.AwsAccessKeyID, t.AwsSecretAccessKey}
					for _, opt := range []string{t.AwsDefaultRegion, t.AwsSignatureVersion, t.EndpointURL} {
						if opt == "" {
							break
						}
						authArgs = append(authArgs, opt)
					}
					inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: authArgs})
					mutations = append(mutations, fmt.Sprintf("%s:backup-auth %s (%s drift)", t.Service, t.Name, strings.Join(fields, ", ")))
					drift = append(drift, fmt.Sprintf("auth (%s)", strings.Join(fields, ", ")))
				}
			}

			if t.EncryptionPassphrase != "" && infoValue(info, "backup-encryption-fingerprint") != backupFingerprint(t.EncryptionPassphrase) {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-set-encryption", t.Service), t.Name, t.EncryptionPassphrase},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-set-encryption %s", t.Service, t.Name))
				drift = append(drift, "encryption passphrase")
			}

			if t.Schedule != "" {
				if fields := backupScheduleDrift(t, info); len(fields) > 0 {
					schedArgs := []string{"--quiet", fmt.Sprintf("%s:backup-schedule", t.Service), t.Name, t.Schedule, t.Bucket}
					mutation := fmt.Sprintf("%s:backup-schedule %s %s %s", t.Service, t.Name, t.Schedule, t.Bucket)
					if t.UseIam {
						schedArgs = append(schedArgs, "--use-iam")
						mutation += " --use-iam"
					}
					inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: schedArgs})
					mutations = append(mutations, mutation)
					drift = append(drift, fmt.Sprintf("schedule (%s)", strings.Join(fields, ", ")))
				}
			}

			if len(inputs) == 0 {
				return observedServiceBackup(t, PlanResult{InSync: true, Status: PlanStatusOK}, info)
			}
			return observedServiceBackup(t, PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    fmt.Sprintf("backup drift on %s: %s", target, strings.Join(drift, "; ")),
				Mutations: mutations,
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}, info)
		},
		StateAbsent: func() PlanResult {
			info, result := probe()
			if result != nil {
				return *result
			}

			inputs := []subprocess.ExecCommandInput{}
			mutations := []string{}
			present := []string{}

			if infoValue(info, "backup-schedule") != "" {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-unschedule", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-unschedule %s", t.Service, t.Name))
				present = append(present, "schedule")
			}
			// backup-deauth removes every auth setting at once, so a single
			// stored credential or a region left behind is enough to run it.
			if backupAuthStored(info) {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-deauth", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-deauth %s", t.Service, t.Name))
				present = append(present, "auth")
			}
			if infoValue(info, "backup-encryption-fingerprint") != "" {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-unset-encryption", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-unset-encryption %s", t.Service, t.Name))
				present = append(present, "encryption passphrase")
			}

			if len(inputs) == 0 {
				return observedServiceBackup(t, PlanResult{InSync: true, Status: PlanStatusOK}, info)
			}
			return observedServiceBackup(t, PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("backup %s configured on %s", strings.Join(present, ", "), target),
				Mutations: mutations,
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}, info)
		},
	})
}

// infoValue returns the trimmed value info reports for key.
func infoValue(info map[string]string, key string) string {
	return strings.TrimSpace(info[key])
}

// backupAuthDrift names the auth settings info reports differently from the
// recipe. The credentials are compared by fingerprint. An undeclared region,
// signature version or endpoint must read back empty, since backup-auth
// clears any setting it is not given.
func backupAuthDrift(t ServiceBackupTask, info map[string]string) []string {
	var fields []string
	if infoValue(info, "backup-access-key-id-fingerprint") != backupFingerprint(t.AwsAccessKeyID) {
		fields = append(fields, "access key id")
	}
	if infoValue(info, "backup-secret-access-key-fingerprint") != backupFingerprint(t.AwsSecretAccessKey) {
		fields = append(fields, "secret access key")
	}
	for _, f := range []struct{ key, desired, label string }{
		{"backup-default-region", t.AwsDefaultRegion, "region"},
		{"backup-signature-version", t.AwsSignatureVersion, "signature version"},
		{"backup-endpoint-url", t.EndpointURL, "endpoint url"},
	} {
		if infoValue(info, f.key) != f.desired {
			fields = append(fields, f.label)
		}
	}
	return fields
}

// backupScheduleDrift names the schedule settings info reports differently
// from the recipe.
func backupScheduleDrift(t ServiceBackupTask, info map[string]string) []string {
	var fields []string
	if infoValue(info, "backup-schedule") != t.Schedule {
		fields = append(fields, "schedule")
	}
	if infoValue(info, "backup-bucket") != t.Bucket {
		fields = append(fields, "bucket")
	}
	if (infoValue(info, "backup-use-iam") == "true") != t.UseIam {
		fields = append(fields, "use_iam")
	}
	return fields
}

// backupAuthStored reports whether info shows any auth setting stored: either
// credential, the region, the signature version or the endpoint.
func backupAuthStored(info map[string]string) bool {
	for _, key := range []string{
		"backup-access-key-id-fingerprint",
		"backup-secret-access-key-fingerprint",
		"backup-default-region",
		"backup-signature-version",
		"backup-endpoint-url",
	} {
		if infoValue(info, key) != "" {
			return true
		}
	}
	return false
}

// observedServiceBackup attaches the backup settings the info report carries to r.
// The credentials and the passphrase are reported only as fingerprints, so
// they stay empty; the region, signature version and endpoint are reported
// as stored, so Observed carries them even though export, which cannot emit
// them without the credentials, does not. The resource exists when any
// setting is stored: a schedule, an auth setting, or an encryption
// passphrase.
func observedServiceBackup(t ServiceBackupTask, r PlanResult, info map[string]string) PlanResult {
	o := ServiceBackupTask{
		Service:             t.Service,
		Name:                t.Name,
		Schedule:            infoValue(info, "backup-schedule"),
		Bucket:              infoValue(info, "backup-bucket"),
		UseIam:              infoValue(info, "backup-use-iam") == "true",
		AwsDefaultRegion:    infoValue(info, "backup-default-region"),
		AwsSignatureVersion: infoValue(info, "backup-signature-version"),
		EndpointURL:         infoValue(info, "backup-endpoint-url"),
		State:               StateAbsent,
	}
	exists := o.Schedule != "" || backupAuthStored(info) || infoValue(info, "backup-encryption-fingerprint") != ""
	if exists {
		o.State = StatePresent
	}
	return observed(r, o, exists)
}

// init registers the ServiceBackupTask with the task registry
func init() {
	RegisterTask(&ServiceBackupTask{})
}
