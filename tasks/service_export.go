package tasks

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
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
func listServices() ([]serviceInstance, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
func serviceLinkedApps(service, name string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
func serviceDSN(service, name string) (string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

// linkedServiceDSNs returns the set of datastore DSNs that service links have
// injected into an app's config, so the config exporter can omit them: the
// dokku_service_link task recreates those `<ALIAS>_URL` vars on apply (with the
// new server's credentials), and re-exporting the stale value would clobber the
// fresh one. Enumerates services, keeps those linked to the app, and reads each
// linked service's DSN.
func linkedServiceDSNs(app string) (map[string]bool, error) {
	services, err := listServices()
	if err != nil {
		return nil, err
	}
	dsns := map[string]bool{}
	for _, s := range services {
		apps, err := serviceLinkedApps(s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if !apps[app] {
			continue
		}
		dsn, err := serviceDSN(s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if dsn != "" {
			dsns[dsn] = true
		}
	}
	return dsns, nil
}
