package tasks

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// CertsTask manages SSL certificates for a dokku app or globally
type CertsTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" identity:"key" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the certificate should be applied globally
	// via the dokku-global-cert plugin
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Flag indicating if the certificate should be applied globally via the dokku-global-cert plugin"`

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t CertsTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "the installed certificate is compared against the desired one via certs:show, but the private key is never read back, so a key rotated under an unchanged certificate plans as in sync; a cert file path is only compared when docket can read the file from the machine it runs on, which a run against --host cannot; and a letsencrypt-managed certificate is left uncompared"}
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
func (t CertsTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
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
func (t CertsTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			hasContent := t.CertContent != "" && t.KeyContent != ""
			enabled, err := certsEnabled(ctx, t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			drifted := false
			if enabled {
				drifted, err = certsMaterialDrifted(ctx, t)
				if err != nil {
					return PlanResult{Status: PlanStatusError, Error: err}
				}
				if !drifted {
					return PlanResult{InSync: true, Status: PlanStatusOK}
				}
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
			if drifted {
				// certs:add and certs:update are the same command in dokku, and
				// both fn-certs-set and fn-global-cert-set overwrite what is
				// there, so replacing a certificate needs no command of its own.
				return PlanResult{
					InSync:    false,
					Status:    PlanStatusModify,
					Reason:    "certificate material drift",
					Mutations: []string{fmt.Sprintf("replace certificate for %s", target)},
					Commands:  resolveCommands(ctx, inputs),
					apply: func(ctx context.Context) TaskOutputState {
						return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StatePresent, inputs)
					},
				}
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "certificate not installed",
				Mutations: []string{fmt.Sprintf("install certificate for %s", target)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			enabled, err := certsEnabled(ctx, t)
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
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
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
func certsEnabled(ctx context.Context, t CertsTask) (bool, error) {
	args := []string{"--quiet", "certs:report", t.App, "--ssl-enabled"}
	if t.Global {
		// The `--global` scope is required: dokku-global-cert standardized
		// global-cert:report so a bare info flag now reports per-app; `--global`
		// targets the global certificate itself, which returns "true"/"false".
		args = []string{"--quiet", "global-cert:report", "--global", "--global-cert-enabled"}
	}

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// certsMaterialDrifted reports whether the certificate installed for the task's
// scope is a different one from the certificate the recipe pins. It answers true
// only on positive evidence: material read back from the server that does not
// match the desired material. Everything it cannot see reads as false, so a
// scope it cannot compare keeps the coarse "a certificate is installed, so we
// are in sync" answer Plan() gave before it compared anything.
//
// Two cases are deliberately not compared. Desired material docket cannot read
// leaves nothing to compare against - see desiredCertPEM. And a
// letsencrypt-managed certificate is left alone for the reason `docket export`
// skips one (#337): it is ephemeral and re-issued on renewal, so treating a
// fresh one as drift would have docket overwrite it with the recipe's stale pin
// on every run. A non-SSH error from that probe means the letsencrypt plugin is
// not installed, which is itself the answer that the certificate is not
// letsencrypt-managed, so the comparison goes ahead.
//
// The private key is never read. Comparing the certificate catches renewal,
// which is what re-running one of these tasks is almost always about; moving key
// material off the server to also catch a key rotated under an unchanged
// certificate is not a trade this makes.
func certsMaterialDrifted(ctx context.Context, t CertsTask) (bool, error) {
	desired, ok := desiredCertPEM(ctx, t)
	if !ok {
		return false, nil
	}

	if !t.Global {
		active, err := letsencryptActive(ctx, t.App)
		if err != nil {
			var sshErr *subprocess.SSHError
			if errors.As(err, &sshErr) {
				return false, err
			}
		} else if active {
			return false, nil
		}
	}

	installed, err := certsShow(ctx, t, "crt")
	if err != nil {
		return false, err
	}
	return !samePEM(installed, desired), nil
}

// desiredCertPEM returns the certificate PEM the recipe pins, and whether docket
// can read it from where it runs. Inline cert_content is always readable. The
// `cert` field is a path on the dokku server, which is this machine's path only
// when the run is local, so a run carrying a target host reports false rather
// than comparing against whatever a local file of that name happens to hold. A
// file docket's own user cannot read - what `--sudo` against a root-owned key
// directory looks like - reports false for the same reason: no desired material,
// nothing to compare.
func desiredCertPEM(ctx context.Context, t CertsTask) (string, bool) {
	if t.CertContent != "" {
		return t.CertContent, true
	}
	if t.Cert == "" || subprocess.TargetFromContext(ctx).Host != "" {
		return "", false
	}
	data, err := os.ReadFile(t.Cert)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// samePEM reports whether two PEM documents carry the same certificate chain.
// The comparison is over each block's type and decoded DER, in order, so a
// trailing newline, CRLF line endings or different line wrapping between what a
// recipe pins and what certs:show hands back - StdoutContents trims it - do not
// read as drift. Bytes outside the blocks are ignored, which is what makes an
// openssl text dump printed ahead of the certificate harmless.
//
// A document holding no PEM block at all is not something this can normalise, so
// such a pair falls back to a whitespace-trimmed string comparison rather than
// comparing equal on the strength of two empty block lists.
func samePEM(a, b string) bool {
	blocksA, okA := pemBlocks(a)
	blocksB, okB := pemBlocks(b)
	if !okA || !okB {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	if len(blocksA) != len(blocksB) {
		return false
	}
	for i := range blocksA {
		if blocksA[i].Type != blocksB[i].Type || !bytes.Equal(blocksA[i].Bytes, blocksB[i].Bytes) {
			return false
		}
	}
	return true
}

// pemBlocks decodes every PEM block in s, reporting false when it holds none.
func pemBlocks(s string) ([]*pem.Block, bool) {
	var blocks []*pem.Block
	rest := []byte(s)
	for {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
		rest = remainder
	}
	return blocks, len(blocks) > 0
}

// ExportApp reconstructs an app's SSL certificate via certs:show. The cert and
// key PEM are sensitive and lifted into the vars-file by the engine.
func (t CertsTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportCert(ctx, CertsTask{App: app})
}

// ExportGlobal reconstructs the global SSL certificate via global-cert:show
// (dokku-global-cert 0.4.x+). The cert and key PEM are sensitive and lifted into
// the vars-file by the engine.
func (t CertsTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportCert(ctx, CertsTask{Global: true})
}

// exportCert probes whether a certificate is installed for the given scope (app
// or global) and, if so, reads its PEM back via certs:show / global-cert:show.
// A transport failure aborts the export; any other probe error (for example the
// dokku-global-cert plugin not being installed) is swallowed so the scope is
// skipped silently.
func exportCert(ctx context.Context, t CertsTask) ([]interface{}, error) {
	enabled, err := certsEnabled(ctx, t)
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
		active, lerr := letsencryptActive(ctx, t.App)
		if lerr != nil {
			var sshErr *subprocess.SSHError
			if errors.As(lerr, &sshErr) {
				return nil, lerr
			}
		} else if active {
			return nil, nil
		}
	}

	crt, err := certsShow(ctx, t, "crt")
	if err != nil {
		return nil, err
	}
	key, err := certsShow(ctx, t, "key")
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
func certsShow(ctx context.Context, t CertsTask, kind string) (string, error) {
	args := []string{"--quiet", "certs:show", t.App, kind}
	if t.Global {
		args = []string{"--quiet", "global-cert:show", kind}
	}
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
