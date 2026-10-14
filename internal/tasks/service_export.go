package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// serviceInstance identifies a single datastore service instance discovered on
// the server: its datastore type (the plugin command prefix, e.g. "postgres")
// and its instance name.
type serviceInstance struct {
	Type string
	Name string
}

// listServices enumerates every datastore service instance on the server.
//
// There is no single "list all services" command, but every datastore plugin
// implements the `service-list` plugin trigger, so `plugin:trigger service-list`
// fans the trigger out across all installed datastore plugins, each echoing
// `<type>:<name>` for its own instances. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level failure (no datastore
// plugin installed, or an older dokku) degrades to "no services."
func listServices(ctx context.Context) ([]serviceInstance, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:trigger", "service-list"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	var services []serviceInstance
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Each line is `<type>:<name>`; service names cannot contain a colon,
		// so split on the first one.
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		services = append(services, serviceInstance{Type: parts[0], Name: parts[1]})
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Type != services[j].Type {
			return services[i].Type < services[j].Type
		}
		return services[i].Name < services[j].Name
	})
	return services, nil
}

// serviceLinkedApps returns the set of apps linked to a datastore service,
// read from `<service>:links <name>` (one app per line on stdout). A
// transport-level failure is propagated; a dokku-level failure (the service is
// gone) degrades to "no links."
func serviceLinkedApps(ctx context.Context, service, name string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", fmt.Sprintf("%s:links", service), name},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]bool{}, nil
	}
	apps := map[string]bool{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		if app := strings.TrimSpace(line); app != "" {
			apps[app] = true
		}
	}
	return apps, nil
}

// serviceDSN returns a datastore service's connection string (the value a link
// injects into an app's config), read from `<service>:info <name> --dsn`. A
// transport-level failure is propagated; any other failure yields an empty DSN.
func serviceDSN(ctx context.Context, service, name string) (string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", fmt.Sprintf("%s:info", service), name, "--dsn"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return "", err
		}
		return "", nil
	}
	return result.StdoutContents(), nil
}

// datastorePluginFloor names the oldest datastore plugin release whose
// `<service>:info` reports every key the service tasks read: dokku-datastore
// 0.7.1, which every datastore plugin bundles from 2.3.1. It is the release
// that fingerprints each backup credential on its own, the last key the
// tasks need.
const datastorePluginFloor = "a datastore plugin >= 2.3.1 (dokku-datastore 0.7.1)"

// datastorePluginRequirement is the MinimumVersions text every task reading
// serviceInfo shares.
const datastorePluginRequirement = "a dokku datastore service plugin >= 2.3.1 matching the service type (dokku-datastore 0.7.1, e.g. dokku-postgres, dokku-redis, dokku-mysql)"

// serviceInfo reads every key a datastore service reports, from one
// `<service>:info <name> --format json` call. The payload is a single flat
// object of strings, keyed by the info flag without its leading dashes.
//
// Failure is decided by the exit code alone. Older plugins write their error
// as a JSON object on stderr with the message elsewhere, and some print it
// twice, so stderr is never parsed. A transport-level failure
// (`*subprocess.SSHError`) is propagated as-is; any other non-zero exit, or a
// payload that does not decode, names the plugin floor, since a plugin older
// than it is the likely cause.
//
// The payload carries `dsn` and `exposed-dsn`, both holding the service
// password. Callers read only the keys they compare, and neither of those ever
// reaches a reason, a warning or a mutation.
func serviceInfo(ctx context.Context, service, name string) (map[string]string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", fmt.Sprintf("%s:info", service), name, "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, fmt.Errorf("%s:info %s --format json failed, which needs %s: %w", service, name, datastorePluginFloor, err)
	}
	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("%s:info %s --format json did not return a report, which needs %s: %w", service, name, datastorePluginFloor, err)
	}
	return payload, nil
}

// backupFingerprint is the lowercase hex sha256 of the values joined by
// newlines, with no trailing newline - the digest dokku-datastore reports for
// a stored backup secret. One value is what `printf '%s' "$a" | sha256sum`
// prints. Comparing a candidate's digest against the reported one says whether
// setting it would change anything without the stored value being printed.
func backupFingerprint(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return hex.EncodeToString(sum[:])
}

// parseCustomEnv reads the `custom-env` value info reports back into a map.
// dokku-datastore stores one `KEY=value` pair per line and reports them
// joined with `;`, the form `--custom-env` takes. dokku_service_create's
// Validate refuses `;` in a value and `=` or `;` in a key, so anything docket
// wrote splits back apart unambiguously; a pair without `=` is kept with an
// empty value rather than dropped, so it still reads as drift.
func parseCustomEnv(value string) map[string]string {
	env := map[string]string{}
	for _, pair := range strings.Split(value, ";") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		env[k] = v
	}
	return env
}

// splitImageRef splits a docker image reference into its name and tag. The
// separator is the last colon, but only when it falls after the last slash:
// in `registry.example.com:5000/postgres` that colon is a registry port, not a
// tag. A reference carrying no tag yields an empty version.
func splitImageRef(ref string) (string, string) {
	colon := strings.LastIndex(ref, ":")
	if colon < 0 || colon < strings.LastIndex(ref, "/") {
		return ref, ""
	}
	return ref[:colon], ref[colon+1:]
}

// linkedServiceDSNs returns the set of datastore DSNs that service links have
// injected into an app's config, so the config exporter can omit them and
// dokku_config's set and clear states leave them in place: the
// dokku_service_link task recreates those `<ALIAS>_URL` vars on apply (with the
// new server's credentials), and re-exporting the stale value would clobber the
// fresh one. Enumerates services, keeps those linked to the app, and reads each
// linked service's DSN.
func linkedServiceDSNs(ctx context.Context, app string) (map[string]bool, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	dsns := map[string]bool{}
	for _, s := range services {
		apps, err := serviceLinkedApps(ctx, s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if !apps[app] {
			continue
		}
		dsn, err := serviceDSN(ctx, s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if dsn != "" {
			dsns[dsn] = true
		}
	}
	return dsns, nil
}

// serviceDSNBody returns a DSN with its `scheme://` prefix removed, leaving the
// credentials, host, port and database that identify the service instance.
func serviceDSNBody(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		return dsn[i+len("://"):]
	}
	return dsn
}

// isLinkedServiceValue reports whether a config value was written by a service
// link, given the DSNs of the services linked to the app (see
// linkedServiceDSNs). Dokku keeps no record of which key a link wrote; the
// datastore plugins find a link's keys by value, treating any key whose value
// contains the service URL as linked, and `<service>:unlink` removes exactly
// those. This mirrors that rule with the scheme dropped from both sides: a link
// renders the URL with the app's `<PLUGIN>_DATABASE_SCHEME` override when one
// is set, and may append a `?querystring`, neither of which `<service>:info
// --dsn` reflects. The body left carries the service's generated password and
// its `dokku-<type>-<name>` host, so a DSN for an external database never
// matches. An empty body never matches either, as it would otherwise claim
// every value.
func isLinkedServiceValue(value string, dsns map[string]bool) bool {
	for dsn := range dsns {
		body := serviceDSNBody(dsn)
		if body != "" && strings.Contains(value, body) {
			return true
		}
	}
	return false
}
