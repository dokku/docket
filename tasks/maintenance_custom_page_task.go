package tasks

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// MaintenanceCustomPageTask installs (or removes) a custom maintenance page for
// a dokku app via the dokku-maintenance plugin. The page is delivered as a tar
// archive streamed to `dokku maintenance:custom-page <app>` on stdin, either
// built from inline HTML (Content) or read from a tarball on disk (Tarball).
type MaintenanceCustomPageTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Content is inline HTML stored as maintenance.html. Mutually exclusive
	// with Tarball.
	Content string `required:"false" yaml:"content,omitempty" description:"Inline HTML stored as maintenance.html on the app. Mutually exclusive with tarball; one is required when state is present."`

	// Tarball is the path on the machine running docket to a tar archive
	// containing at least maintenance.html. Mutually exclusive with Content.
	Tarball string `required:"false" yaml:"tarball,omitempty" description:"Path on the machine running docket to a tar archive containing at least maintenance.html. Mutually exclusive with content; one is required when state is present."`

	// State is the desired state of the custom maintenance page
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the custom maintenance page"`
}

// MaintenanceCustomPageTaskExample contains an example of a MaintenanceCustomPageTask
type MaintenanceCustomPageTaskExample struct {
	// Name is the task name holding the MaintenanceCustomPageTask description
	Name string `yaml:"-"`

	// MaintenanceCustomPageTask is the MaintenanceCustomPageTask configuration
	MaintenanceCustomPageTask MaintenanceCustomPageTask `yaml:"dokku_maintenance_custom_page"`
}

// GetName returns the name of the example
func (e MaintenanceCustomPageTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the maintenance custom page task
func (t MaintenanceCustomPageTask) Doc() string {
	return "Installs or removes a custom maintenance page for a dokku application."
}

// ExportSupport reports how docket export handles this task.
func (t MaintenanceCustomPageTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "export reads the current page back via maintenance:custom-page-export and inlines maintenance.html as content. Multi-file tarball pages collapse to that single content field, so extra assets are not captured. On an older dokku-maintenance without the export command the content cannot be read back and is lifted into a required content input the user supplies before apply"}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t MaintenanceCustomPageTask) Requirements() []string {
	return []string{"dokku-maintenance plugin"}
}

// Examples returns the examples for the maintenance custom page task
func (t MaintenanceCustomPageTask) Examples() ([]Doc, error) {
	return MarshalExamples([]MaintenanceCustomPageTaskExample{
		{
			Name: "Set a custom maintenance page from inline HTML",
			MaintenanceCustomPageTask: MaintenanceCustomPageTask{
				App:     "node-js-app",
				Content: "<html><body><h1>Down for maintenance</h1></body></html>\n",
			},
		},
		{
			Name: "Set a custom maintenance page from a tarball (supports extra assets)",
			MaintenanceCustomPageTask: MaintenanceCustomPageTask{
				App:     "node-js-app",
				Tarball: "/etc/dokku/maintenance/node-js-app.tar",
			},
		},
		{
			Name: "Remove the custom maintenance page, resetting to the default",
			MaintenanceCustomPageTask: MaintenanceCustomPageTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute installs or removes the custom maintenance page
func (t MaintenanceCustomPageTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the MaintenanceCustomPageTask's inputs without contacting the server.
func (t MaintenanceCustomPageTask) Validate() error {
	if err := validateMaintenanceCustomPageTask(t); err != nil {
		return err
	}
	if t.State == StatePresent && t.Content == "" && t.Tarball == "" {
		return fmt.Errorf("one of 'content' or 'tarball' is required when state is 'present'")
	}
	if t.State == StateAbsent && (t.Content != "" || t.Tarball != "") {
		return fmt.Errorf("'content' and 'tarball' must not be set when state is 'absent'")
	}
	return nil
}

// Plan reports the drift the MaintenanceCustomPageTask would produce.
func (t MaintenanceCustomPageTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			tarBytes, err := maintenanceCustomPageTarball(t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			desired, err := maintenanceTarballChecksum(tarBytes)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}

			current, reported, err := maintenanceCustomPageState(t.App)
			if err != nil {
				var sshErr *subprocess.SSHError
				if errors.As(err, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: err}
				}
			}
			if err == nil && reported && current == desired {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			input := subprocess.ExecCommandInput{
				Command: "dokku",
				Args:    []string{"--quiet", "maintenance:custom-page", t.App},
				Stdin:   bytes.NewReader(tarBytes),
			}
			inputs := []subprocess.ExecCommandInput{input}

			status := PlanStatusModify
			reason := fmt.Sprintf("custom page drift for %s", t.App)
			switch {
			case err != nil:
				reason = fmt.Sprintf("custom page for %s not readable from server (probe failed: %v)", t.App, err)
			case !reported:
				status = PlanStatusCreate
				reason = fmt.Sprintf("custom page checksum not reported for %s", t.App)
			case current == "":
				status = PlanStatusCreate
				reason = fmt.Sprintf("custom page not set for %s", t.App)
			}

			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("set custom maintenance page for %s", t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			current, reported, err := maintenanceCustomPageState(t.App)
			if err != nil {
				var sshErr *subprocess.SSHError
				if errors.As(err, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: err}
				}
			}
			if err == nil && reported && current == "" {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: []string{"--quiet", "maintenance:custom-page-remove", t.App}}}

			reason := fmt.Sprintf("custom page present for %s", t.App)
			if err != nil {
				reason = fmt.Sprintf("custom page for %s not readable from server (probe failed: %v)", t.App, err)
			} else if !reported {
				reason = fmt.Sprintf("custom page checksum not reported for %s", t.App)
			}

			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("remove custom maintenance page for %s", t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// validateMaintenanceCustomPageTask validates the task parameters shared by
// both states.
func validateMaintenanceCustomPageTask(t MaintenanceCustomPageTask) error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.Content != "" && t.Tarball != "" {
		return fmt.Errorf("'content' and 'tarball' are mutually exclusive")
	}
	return nil
}

// maintenanceCustomPageTarball resolves the tar bytes that will be streamed to
// the plugin: an in-memory archive built from inline Content, or the raw bytes
// of the on-disk Tarball.
func maintenanceCustomPageTarball(t MaintenanceCustomPageTask) ([]byte, error) {
	if t.Content != "" {
		tarBytes, err := buildMaintenancePageTarball(t.Content)
		if err != nil {
			return nil, fmt.Errorf("build maintenance page tarball: %w", err)
		}
		return tarBytes, nil
	}
	tarBytes, err := os.ReadFile(t.Tarball)
	if err != nil {
		return nil, fmt.Errorf("read tarball %q: %w", t.Tarball, err)
	}
	return tarBytes, nil
}

// buildMaintenancePageTarball produces an uncompressed tar archive containing a
// single maintenance.html entry with the supplied HTML. The plugin extracts
// this archive from stdin and serves maintenance.html as the custom page.
func buildMaintenancePageTarball(content string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "maintenance.html",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// maintenanceTarballChecksum reproduces the dokku-maintenance plugin's canonical
// digest (fn-maintenance-custom-page-checksum) over the files a tar archive
// would extract to. For each regular-file entry it emits a
// "<sha256hex(contents)>  <relpath>\n" line (two spaces), sorts the lines by
// relpath in byte order, concatenates them, and returns the sha256 hex of the
// result. Because the digest is over the extracted content (not the raw tar
// bytes, which embed mtimes and ordering) it is reproducible client-side, which
// is what lets Plan() skip a redundant upload. The archive must contain a
// root-level maintenance.html, matching the plugin's own requirement.
//
// Only regular files are hashed, matching the plugin's `find -type f`. Note the
// plugin moves the extracted tree with `mv $TEMP_DIR/*`, whose glob omits
// root-level dotfiles; such a file would make this digest differ from the
// server's and force a re-upload every run (safe, just non-idempotent for that
// rare case). That quirk is intentionally not replicated here.
func maintenanceTarballChecksum(tarBytes []byte) (string, error) {
	type entry struct {
		name string
		sum  string
	}
	var entries []entry
	hasIndex := false

	tr := tar.NewReader(bytes.NewReader(tarBytes))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		h := sha256.New()
		if _, err := io.Copy(h, tr); err != nil {
			return "", fmt.Errorf("read tarball entry %q: %w", name, err)
		}
		if name == "maintenance.html" {
			hasIndex = true
		}
		entries = append(entries, entry{name: name, sum: hex.EncodeToString(h.Sum(nil))})
	}

	if !hasIndex {
		return "", fmt.Errorf("tarball must contain a root-level maintenance.html")
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var stream bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&stream, "%s  %s\n", e.sum, e.name)
	}
	final := sha256.Sum256(stream.Bytes())
	return hex.EncodeToString(final[:]), nil
}

// maintenanceCustomPageState reads the current custom page checksum from
// `dokku maintenance:report <app> --format json`. The plugin strips the
// `maintenance-` prefix from JSON report keys, so the checksum lands under
// `custom-page-sha256` (empty string when no custom page is set). The key is
// decoded into a *string so a missing key (a plugin too old to report it) is
// distinguishable from a reported-but-empty value: reported is false only when
// the key is absent. A transport-level SSH failure is returned as an error so
// Plan() can short-circuit; a dokku-level failure is also returned so callers
// can fall back to applying unconditionally.
func maintenanceCustomPageState(app string) (checksum string, reported bool, err error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"maintenance:report", app, "--format", "json"},
	})
	if err != nil {
		return "", false, err
	}
	var report struct {
		CustomPageSHA256 *string `json:"custom-page-sha256"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return "", false, err
	}
	if report.CustomPageSHA256 == nil {
		return "", false, nil
	}
	return *report.CustomPageSHA256, true, nil
}

// ExportApp satisfies AppExporter by delegating to exportApp with a no-op warn
// callback. The export engine prefers ExportAppReport when present, so the
// dropped-assets diagnostic reaches ExportReport.Warnings rather than being
// discarded here.
func (t MaintenanceCustomPageTask) ExportApp(app string) ([]interface{}, error) {
	return t.exportApp(app, func(string) {})
}

// ExportAppReport is the diagnostics-aware form of ExportApp (the
// appExportReporter interface): it routes the "dropped extra assets" warning
// through the engine's warn callback (wired to ExportReport.Warnings) instead
// of a raw log line, so the warning is rendered and masked like every other
// export diagnostic.
func (t MaintenanceCustomPageTask) ExportAppReport(app string, warn func(msg string)) ([]interface{}, error) {
	return t.exportApp(app, warn)
}

// exportApp emits a dokku_maintenance_custom_page task when the app has a custom
// page installed. A custom page is detected by a non-empty custom-page-sha256 in
// maintenance:report, so nothing is emitted when no page is set or when the
// plugin is too old to report the checksum. The page content is then read back
// via maintenance:custom-page-export and inlined as Content, producing a faithful,
// self-contained task. On an older dokku-maintenance without the export command
// (any non-SSH failure) the content cannot be read; an empty task is returned and
// processMaintenanceCustomPage lifts the content into a required input. A
// transport-level SSH failure propagates as a warning. When the archive carries
// files beyond maintenance.html (which the single-Content task cannot represent)
// warn is invoked with the dropped-assets notice.
func (t MaintenanceCustomPageTask) exportApp(app string, warn func(msg string)) ([]interface{}, error) {
	checksum, reported, err := maintenanceCustomPageState(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}
	if !reported || checksum == "" {
		return nil, nil
	}

	content, extraAssets, err := maintenanceCustomPageExport(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return []interface{}{MaintenanceCustomPageTask{App: app}}, nil
	}
	if extraAssets {
		warn("custom page has files beyond maintenance.html; only maintenance.html is captured in the export")
	}
	return []interface{}{MaintenanceCustomPageTask{App: app, Content: content}}, nil
}

// maintenanceCustomPageExport reads the app's current custom page back via
// `dokku maintenance:custom-page-export <app>` (the inverse of
// maintenance:custom-page), which streams the stored tar archive to stdout. It
// returns the contents of the root-level maintenance.html and whether the archive
// carried any additional files (which the single-content task shape cannot
// represent, so they are dropped - see ExportSupport).
func maintenanceCustomPageExport(app string) (content string, extraAssets bool, err error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "maintenance:custom-page-export", app},
	})
	if err != nil {
		return "", false, err
	}

	// Read the raw stdout bytes, not StdoutBytes(), which trims whitespace and
	// would corrupt a binary tar archive.
	found := false
	tr := tar.NewReader(bytes.NewReader([]byte(result.Stdout)))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, fmt.Errorf("read custom page tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.TrimPrefix(hdr.Name, "./") == "maintenance.html" {
			b, err := io.ReadAll(tr)
			if err != nil {
				return "", false, fmt.Errorf("read maintenance.html: %w", err)
			}
			content = string(b)
			found = true
			continue
		}
		extraAssets = true
	}
	if !found {
		return "", false, fmt.Errorf("custom page tarball has no maintenance.html")
	}
	return content, extraAssets, nil
}

// init registers the MaintenanceCustomPageTask with the task registry
func init() {
	RegisterTask(&MaintenanceCustomPageTask{})
}
