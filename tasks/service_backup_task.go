package tasks

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// ServiceBackupTask manages the S3 backup configuration for a dokku
// datastore service: the backup schedule, the S3 authentication, and the
// backup encryption passphrase.
//
// Only the schedule is idempotent: dokku can read it back via
// <service>:backup-schedule-cat. The auth and encryption pieces have no
// read command, so when those fields are provided they are applied
// unconditionally and the task reports Changed=true, mirroring the
// no-probe pattern used by dokku_git_auth and dokku_registry_auth.
type ServiceBackupTask struct {
	// Service is the type of service to back up (e.g. redis, postgres, mysql)
	Service string `required:"true" yaml:"service" description:"Type of service to back up (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" yaml:"name" description:"Name of the service instance"`

	// Schedule is the cron schedule for backups. Requires bucket.
	Schedule string `required:"false" yaml:"schedule,omitempty" description:"Cron schedule for backups (requires bucket)"`

	// Bucket is the S3 bucket backups are written to. Requires schedule.
	Bucket string `required:"false" yaml:"bucket,omitempty" description:"S3 bucket backups are written to (requires schedule)"`

	// UseIam configures the schedule to use the instance IAM profile instead of static credentials.
	UseIam bool `required:"false" yaml:"use_iam,omitempty" description:"Use the instance IAM profile instead of static credentials"`

	// AwsAccessKeyID is the AWS access key id used for backup auth. Requires aws_secret_access_key.
	AwsAccessKeyID string `required:"false" yaml:"aws_access_key_id,omitempty" description:"AWS access key id for backup auth (requires aws_secret_access_key)"`

	// AwsSecretAccessKey is the AWS secret access key used for backup auth. Requires aws_access_key_id.
	AwsSecretAccessKey string `required:"false" sensitive:"true" yaml:"aws_secret_access_key,omitempty" description:"AWS secret access key for backup auth (requires aws_access_key_id)"`

	// AwsDefaultRegion is the AWS region used for backup auth.
	AwsDefaultRegion string `required:"false" yaml:"aws_default_region,omitempty" description:"AWS region used for backup auth"`

	// AwsSignatureVersion is the AWS signature version used for backup auth.
	AwsSignatureVersion string `required:"false" yaml:"aws_signature_version,omitempty" description:"AWS signature version used for backup auth"`

	// EndpointURL is the S3-compatible endpoint url used for backup auth.
	EndpointURL string `required:"false" yaml:"endpoint_url,omitempty" description:"S3-compatible endpoint url used for backup auth"`

	// EncryptionPassphrase is the passphrase used to encrypt future backups.
	EncryptionPassphrase string `required:"false" sensitive:"true" yaml:"encryption_passphrase,omitempty" description:"Passphrase used to encrypt future backups"`

	// State is the desired state of the backup configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the backup configuration"`
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
	return ExportSupport{Status: ExportUnsupported, Caveat: serviceExportCaveat}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t ServiceBackupTask) Requirements() []string {
	return []string{"a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)"}
}

// Examples returns a list of ServiceBackupTaskExamples as yaml
func (t ServiceBackupTask) Examples() ([]Doc, error) {
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
func (t ServiceBackupTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// hasAuth reports whether any AWS credential field was provided.
func (t ServiceBackupTask) hasAuth() bool {
	return t.AwsAccessKeyID != "" || t.AwsSecretAccessKey != ""
}

// Validate checks the ServiceBackupTask's inputs without contacting the server.
func (t ServiceBackupTask) Validate() error {
	if t.State == StatePresent {
		if t.Schedule != "" && t.Bucket == "" {
			return fmt.Errorf("'bucket' is required when 'schedule' is set")
		}
		if t.Bucket != "" && t.Schedule == "" {
			return fmt.Errorf("'schedule' is required when 'bucket' is set")
		}
		if t.hasAuth() && (t.AwsAccessKeyID == "" || t.AwsSecretAccessKey == "") {
			return fmt.Errorf("'aws_access_key_id' and 'aws_secret_access_key' are both required to configure backup auth")
		}
		if t.Schedule == "" && !t.hasAuth() && t.EncryptionPassphrase == "" {
			return fmt.Errorf("at least one of 'schedule', aws credentials, or 'encryption_passphrase' is required when state is 'present'")
		}
	}
	return nil
}

// Plan reports the drift the ServiceBackupTask would produce.
func (t ServiceBackupTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}

			inputs := []subprocess.ExecCommandInput{}
			mutations := []string{}

			// auth and encryption have no read command, so they always run
			// when provided. Keep secrets out of the mutation strings; the
			// resolved Commands mask registered sensitive values.
			if t.hasAuth() {
				authArgs := []string{"--quiet", fmt.Sprintf("%s:backup-auth", t.Service), t.Name, t.AwsAccessKeyID, t.AwsSecretAccessKey}
				for _, opt := range []string{t.AwsDefaultRegion, t.AwsSignatureVersion, t.EndpointURL} {
					if opt == "" {
						break
					}
					authArgs = append(authArgs, opt)
				}
				inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: authArgs})
				mutations = append(mutations, fmt.Sprintf("%s:backup-auth %s", t.Service, t.Name))
			}

			if t.EncryptionPassphrase != "" {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-set-encryption", t.Service), t.Name, t.EncryptionPassphrase},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-set-encryption %s", t.Service, t.Name))
			}

			if t.Schedule != "" {
				content, scheduled, err := serviceBackupScheduled(t.Service, t.Name)
				if err != nil {
					return PlanResult{Status: PlanStatusError, Error: err}
				}
				inSync := scheduled && strings.Contains(content, t.Schedule) && strings.Contains(content, t.Bucket)
				if !inSync {
					schedArgs := []string{"--quiet", fmt.Sprintf("%s:backup-schedule", t.Service), t.Name, t.Schedule, t.Bucket}
					mutation := fmt.Sprintf("%s:backup-schedule %s %s %s", t.Service, t.Name, t.Schedule, t.Bucket)
					if t.UseIam {
						schedArgs = append(schedArgs, "--use-iam")
						mutation += " --use-iam"
					}
					inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: schedArgs})
					mutations = append(mutations, mutation)
				}
			}

			if len(inputs) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "backup configuration not in sync",
				Mutations: mutations,
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
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}

			inputs := []subprocess.ExecCommandInput{}
			mutations := []string{}

			_, scheduled, err := serviceBackupScheduled(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if scheduled {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-unschedule", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-unschedule %s", t.Service, t.Name))
			}
			if t.hasAuth() {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-deauth", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-deauth %s", t.Service, t.Name))
			}
			if t.EncryptionPassphrase != "" {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:backup-unset-encryption", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:backup-unset-encryption %s", t.Service, t.Name))
			}

			if len(inputs) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "backup configuration present",
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// serviceBackupScheduled reports whether a dokku service has a backup
// schedule and returns the cron file contents for comparison. A
// transport-level failure (`*subprocess.SSHError`) is propagated; a
// dokku-level non-zero exit (no schedule configured) is treated as
// "not scheduled."
func serviceBackupScheduled(service, name string) (string, bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:backup-schedule-cat", service),
			name,
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return "", false, err
		}
		return "", false, nil
	}
	return result.StdoutContents(), true, nil
}

// init registers the ServiceBackupTask with the task registry
func init() {
	RegisterTask(&ServiceBackupTask{})
}
