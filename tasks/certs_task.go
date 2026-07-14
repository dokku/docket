package tasks

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// CertsTask manages SSL certificates for a dokku app or globally
type CertsTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the certificate should be applied globally
	// via the dokku-global-cert plugin
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the certificate should be applied globally via the dokku-global-cert plugin"`

	// Cert is the path on the dokku server to the SSL certificate file
	Cert string `required:"false" sensitive:"true" yaml:"cert,omitempty" description:"Path on the dokku server to the SSL certificate file. Mutually exclusive with cert_content."`

	// Key is the path on the dokku server to the SSL certificate key file
	Key string `required:"false" sensitive:"true" yaml:"key,omitempty" description:"Path on the dokku server to the SSL certificate key file. Mutually exclusive with key_content."`

	// CertContent is the PEM-encoded certificate contents. Mutually
	// exclusive with Cert.
	CertContent string `required:"false" sensitive:"true" yaml:"cert_content,omitempty" description:"PEM-encoded certificate contents. Mutually exclusive with cert."`

	// KeyContent is the PEM-encoded private key contents. Mutually
	// exclusive with Key.
	KeyContent string `required:"false" sensitive:"true" yaml:"key_content,omitempty" description:"PEM-encoded private key contents. Mutually exclusive with key."`

	// State is the desired state of the SSL configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the SSL configuration"`
}

// CertsTaskExample contains an example of a CertsTask
type CertsTaskExample struct {
	// Name is the task name holding the CertsTask description
	Name string `yaml:"-"`

	// CertsTask is the CertsTask configuration
	CertsTask CertsTask `yaml:"dokku_certs"`
}

// GetName returns the name of the example
func (e CertsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the certs task
func (t CertsTask) Doc() string {
	return "Manages SSL certificates for a dokku app or globally."
}

// ExportSupport reports how docket export handles this task.
func (t CertsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "app and global certificate PEM material is exported (via certs:show and global-cert:show) and written to the companion vars-file"}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t CertsTask) Requirements() []string {
	return []string{"dokku-global-cert plugin (required only when global: true)"}
}

// Examples returns the examples for the certs task
func (t CertsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]CertsTaskExample{
		{
			Name: "Add an SSL certificate to an app",
			CertsTask: CertsTask{
				App:  "node-js-app",
				Cert: "/etc/nginx/ssl/node-js-app.crt",
				Key:  "/etc/nginx/ssl/node-js-app.key",
			},
		},
		{
			Name: "Remove an SSL certificate from an app",
			CertsTask: CertsTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
		{
			Name: "Add a global SSL certificate (requires the dokku-global-cert plugin)",
			CertsTask: CertsTask{
				Global: true,
				Cert:   "/etc/nginx/ssl/global.crt",
				Key:    "/etc/nginx/ssl/global.key",
			},
		},
		{
			Name: "Remove the global SSL certificate",
			CertsTask: CertsTask{
				Global: true,
				State:  StateAbsent,
			},
		},
		{
			Name: "Add an SSL certificate to an app from inline PEM",
			CertsTask: CertsTask{
				App:         "node-js-app",
				CertContent: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
				KeyContent:  "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
			},
		},
		{
			Name: "Add a global SSL certificate from inline PEM (requires the dokku-global-cert plugin)",
			CertsTask: CertsTask{
				Global:      true,
				CertContent: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----\n",
				KeyContent:  "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
			},
		},
	})
}

// Execute manages the SSL certificate
func (t CertsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the CertsTask's inputs without contacting the server.
func (t CertsTask) Validate() error {
	if err := validateCertsTask(t); err != nil {
		return err
	}
	if t.State == StatePresent {
		hasPaths := t.Cert != "" && t.Key != ""
		hasContent := t.CertContent != "" && t.KeyContent != ""
		if !hasPaths && !hasContent {
			return fmt.Errorf("'cert' (or 'cert_content') and 'key' (or 'key_content') are required when state is 'present'")
		}
	}
	return nil
}

// Plan reports the drift the CertsTask would produce.
func (t CertsTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			hasContent := t.CertContent != "" && t.KeyContent != ""
			enabled, err := certsEnabled(t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			target := t.App
			if t.Global {
				target = "(global)"
			}
			input := subprocess.ExecCommandInput{Command: "dokku"}
			if hasContent {
				tarBytes, err := buildCertTarball(t.CertContent, t.KeyContent)
				if err != nil {
					return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("build cert tarball: %w", err)}
				}
				input.Stdin = bytes.NewReader(tarBytes)
				if t.Global {
					input.Args = []string{"--quiet", "global-cert:set"}
				} else {
					input.Args = []string{"--quiet", "certs:add", t.App}
				}
			} else {
				if t.Global {
					input.Args = []string{"--quiet", "global-cert:set", t.Cert, t.Key}
				} else {
					input.Args = []string{"--quiet", "certs:add", t.App, t.Cert, t.Key}
				}
			}
			inputs := []subprocess.ExecCommandInput{input}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "certificate not installed",
				Mutations: []string{fmt.Sprintf("install certificate for %s", target)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			enabled, err := certsEnabled(t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			target := t.App
			if t.Global {
				target = "(global)"
			}
			args := []string{"--quiet", "certs:remove", t.App}
			if t.Global {
				args = []string{"--quiet", "global-cert:remove"}
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "certificate present",
				Mutations: []string{fmt.Sprintf("remove certificate for %s", target)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// validateCertsTask validates the certs task parameters
func validateCertsTask(t CertsTask) error {
	if t.Global && t.App != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if !t.Global && t.App == "" {
		return fmt.Errorf("'app' is required when 'global' is not set to true")
	}
	if t.Cert != "" && t.CertContent != "" {
		return fmt.Errorf("'cert' and 'cert_content' are mutually exclusive")
	}
	if t.Key != "" && t.KeyContent != "" {
		return fmt.Errorf("'key' and 'key_content' are mutually exclusive")
	}
	if (t.Cert != "" && t.KeyContent != "") || (t.CertContent != "" && t.Key != "") {
		return fmt.Errorf("'cert'/'key' and 'cert_content'/'key_content' cannot be mixed; supply both from the same source")
	}
	return nil
}

// buildCertTarball produces an uncompressed tar archive containing
// server.crt and server.key entries with the supplied PEM contents.
// Both dokku's core certs:add and the dokku-global-cert plugin extract
// such an archive from stdin and select the cert/key by file extension.
func buildCertTarball(certPEM, keyPEM string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	entries := []struct {
		name, body string
	}{
		{"server.crt", certPEM},
		{"server.key", keyPEM},
	}
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: e.name,
			Mode: 0o600,
			Size: int64(len(e.body)),
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// certsEnabled checks if a certificate is currently configured for an app or globally
func certsEnabled(t CertsTask) (bool, error) {
	args := []string{"--quiet", "certs:report", t.App, "--ssl-enabled"}
	if t.Global {
		// The `--global` scope is required: dokku-global-cert standardized
		// global-cert:report so a bare info flag now reports per-app; `--global`
		// targets the global certificate itself, which returns "true"/"false".
		args = []string{"--quiet", "global-cert:report", "--global", "--global-cert-enabled"}
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// ExportApp reconstructs an app's SSL certificate via certs:show. The cert and
// key PEM are sensitive and lifted into the vars-file by the engine.
func (t CertsTask) ExportApp(app string) ([]interface{}, error) {
	return exportCert(CertsTask{App: app})
}

// ExportGlobal reconstructs the global SSL certificate via global-cert:show
// (dokku-global-cert 0.4.x+). The cert and key PEM are sensitive and lifted into
// the vars-file by the engine.
func (t CertsTask) ExportGlobal() ([]interface{}, error) {
	return exportCert(CertsTask{Global: true})
}

// exportCert probes whether a certificate is installed for the given scope (app
// or global) and, if so, reads its PEM back via certs:show / global-cert:show.
// A transport failure aborts the export; any other probe error (for example the
// dokku-global-cert plugin not being installed) is swallowed so the scope is
// skipped silently.
func exportCert(t CertsTask) ([]interface{}, error) {
	enabled, err := certsEnabled(t)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}
	if !enabled {
		return nil, nil
	}

	// A letsencrypt-managed app reports ssl-enabled too, but its certificate is
	// ephemeral (~90 days) and re-issued on the new host by the dokku_letsencrypt
	// task. Pinning the current PEM would embed a stale private key and
	// double-manage the same certificate, so skip the certs export for it (#337).
	// A missing dokku-letsencrypt plugin (a non-SSH probe error) means the cert is
	// not letsencrypt-managed, so fall through and export it as a manual cert.
	if !t.Global {
		active, lerr := letsencryptActive(t.App)
		if lerr != nil {
			var sshErr *subprocess.SSHError
			if errors.As(lerr, &sshErr) {
				return nil, lerr
			}
		} else if active {
			return nil, nil
		}
	}

	crt, err := certsShow(t, "crt")
	if err != nil {
		return nil, err
	}
	key, err := certsShow(t, "key")
	if err != nil {
		return nil, err
	}
	if crt == "" || key == "" {
		return nil, nil
	}
	return []interface{}{CertsTask{App: t.App, Global: t.Global, CertContent: crt, KeyContent: key}}, nil
}

// certsShow returns the scope's server.crt or server.key PEM. The per-app scope
// uses core certs:show; the global scope uses global-cert:show (dokku-global-cert
// 0.4.x+), mirroring the app/global branch in certsEnabled.
func certsShow(t CertsTask, kind string) (string, error) {
	args := []string{"--quiet", "certs:show", t.App, kind}
	if t.Global {
		args = []string{"--quiet", "global-cert:show", kind}
	}
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return "", err
	}
	return result.StdoutContents(), nil
}

// init registers the CertsTask with the task registry
func init() {
	RegisterTask(&CertsTask{})
}
