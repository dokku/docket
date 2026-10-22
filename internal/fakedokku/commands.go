package fakedokku

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// failure is a command that ran and exited non-zero.
type failure struct {
	code   int
	stderr string
}

func fail(code int, format string, args ...any) *failure {
	return &failure{code: code, stderr: " !     " + fmt.Sprintf(format, args...)}
}

// missingApp is what dokku prints, with exit code 20, for an app that does
// not exist.
func missingApp(name string) *failure {
	return fail(20, "App %s does not exist", name)
}

// corePlugins are the plugins a dokku install ships, reported at the dokku
// version.
var corePlugins = []string{
	"00_dokku-standard", "20_events", "app-json", "apps", "builder", "builder-dockerfile",
	"builder-herokuish", "builder-lambda", "builder-nixpacks", "builder-null", "builder-pack",
	"builder-railpack", "buildpacks", "builds", "caddy-vhosts", "certs", "checks", "common",
	"config", "cron", "docker-options", "domains", "enter", "git", "haproxy-vhosts", "logs",
	"network", "nginx-vhosts", "openresty-vhosts", "plugin", "ports", "proxy", "ps", "registry",
	"repo", "resource", "run", "scheduler", "scheduler-docker-local", "scheduler-k3s",
	"scheduler-null", "shell", "ssh-keys", "storage", "trace", "traefik-vhosts",
}

// globalFlags are the dokku flags that may come before a subcommand.
var globalFlags = []string{"--quiet", "--force", "--trace"}

// propertyDefaults are the values a flag-form report prints for a property
// left unset, where dokku computes one.
var propertyDefaults = map[string]map[string]string{
	"git": {"rev-env-var": "GIT_REV"},
}

// dispatch answers one dokku command from the state. known is false for a
// command the Server does not model. The caller holds s.mu.
func (s *Server) dispatch(args []string, stdin []byte) (out string, f *failure, known bool) {
	quiet := false
	for len(args) > 0 && contains(globalFlags, args[0]) {
		if args[0] == "--quiet" {
			quiet = true
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return "", nil, false
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "version":
		if len(rest) == 0 {
			return "dokku version " + s.version, nil, true
		}
		return "", nil, false
	case "plugin:list":
		return s.pluginList(rest)
	case "plugin:trigger":
		if len(rest) == 1 && rest[0] == "service-list" {
			return s.serviceList(), nil, true
		}
		return "", nil, false
	case "apps:exists", "apps:create", "apps:destroy", "apps:list":
		return s.appsCommand(sub, rest)
	case "config:export", "config:set", "config:unset", "config:import":
		return s.configCommand(sub, rest, stdin)
	case "domains:report", "domains:add", "domains:remove", "domains:set", "domains:clear",
		"domains:add-global", "domains:remove-global", "domains:set-global", "domains:clear-global":
		return s.domainsCommand(sub, rest)
	case "ports:report", "ports:add", "ports:remove", "ports:set", "ports:clear":
		return s.portsCommand(sub, rest)
	case "buildpacks:list", "buildpacks:add", "buildpacks:remove", "buildpacks:set", "buildpacks:clear":
		return s.buildpacksCommand(sub, rest)
	case "ps:scale":
		return s.psScale(rest, quiet)
	}

	if p, ok := s.propBySub[sub]; ok {
		return s.setPropertyCommand(p, rest)
	}
	if plugin, ok := strings.CutSuffix(sub, ":report"); ok {
		if p, ok := s.propPlugins[plugin]; ok {
			return s.reportCommand(p, rest)
		}
	}
	if serviceType, command, ok := strings.Cut(sub, ":"); ok {
		if _, ok := s.datastores[serviceType]; ok {
			return s.serviceCommand(serviceType, command, rest)
		}
	}
	return "", nil, false
}

// flags splits args into the flags named in valued (each taking the next
// argument), the boolean flags in bare, and the positional arguments. known
// is false when an argument starts with "--" and is in neither list.
func flags(args []string, valued, bare []string) (map[string]string, []string, bool) {
	set := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		switch {
		case contains(valued, arg):
			if i+1 >= len(args) {
				return nil, nil, false
			}
			set[arg] = args[i+1]
			i++
		case contains(bare, arg):
			set[arg] = "true"
		default:
			return nil, nil, false
		}
	}
	return set, positional, true
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func (s *Server) pluginList(rest []string) (string, *failure, bool) {
	opts, positional, ok := flags(rest, []string{"--format"}, nil)
	if !ok || len(positional) != 0 {
		return "", nil, false
	}
	type entry struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Enabled     bool   `json:"enabled"`
		Core        bool   `json:"core"`
		Description string `json:"description"`
	}
	var entries []entry
	for _, name := range corePlugins {
		entries = append(entries, entry{Name: name, Version: s.version, Enabled: true, Core: true, Description: "dokku core " + name + " plugin"})
	}
	for _, name := range sortedKeys(s.plugins) {
		entries = append(entries, entry{Name: name, Version: s.plugins[name], Enabled: true, Description: "dokku " + name + " plugin"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	switch opts["--format"] {
	case "json":
		body, _ := json.Marshal(entries)
		return string(body), nil, true
	case "":
		lines := make([]string, 0, len(entries))
		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("  %-20s %s enabled    %s", e.Name, e.Version, e.Description))
		}
		return strings.Join(lines, "\n"), nil, true
	}
	return "", nil, false
}

func (s *Server) serviceList() string {
	var lines []string
	for _, serviceType := range sortedKeys(s.services) {
		for _, name := range sortedKeys(s.services[serviceType]) {
			lines = append(lines, serviceType+":"+name)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Server) appsCommand(sub string, rest []string) (string, *failure, bool) {
	if sub == "apps:list" {
		if len(rest) != 0 {
			return "", nil, false
		}
		return strings.Join(sortedKeys(s.apps), "\n"), nil, true
	}
	if len(rest) != 1 || strings.HasPrefix(rest[0], "--") {
		return "", nil, false
	}
	name := rest[0]
	_, exists := s.apps[name]
	switch sub {
	case "apps:exists":
		if !exists {
			return "", missingApp(name), true
		}
	case "apps:create":
		if exists {
			return "", fail(1, "Name is already taken"), true
		}
		s.apps[name] = newApp()
	case "apps:destroy":
		if !exists {
			return "", missingApp(name), true
		}
		delete(s.apps, name)
		for _, services := range s.services {
			for _, svc := range services {
				delete(svc.links, name)
			}
		}
	}
	return "", nil, true
}

func (s *Server) configCommand(sub string, rest []string, stdin []byte) (string, *failure, bool) {
	opts, positional, ok := flags(rest, []string{"--format"}, []string{"--encoded", "--no-restart", "--replace"})
	if !ok || len(positional) == 0 {
		return "", nil, false
	}
	name := positional[0]
	a, exists := s.apps[name]
	switch sub {
	case "config:export":
		if opts["--format"] != "json" || len(positional) != 1 || len(opts) != 1 {
			return "", nil, false
		}
		if !exists {
			return "", missingApp(name), true
		}
		body, _ := json.Marshal(a.config)
		return string(body), nil, true
	case "config:set":
		if opts["--format"] != "" || opts["--replace"] != "" {
			return "", nil, false
		}
		if !exists {
			return "", missingApp(name), true
		}
		values := map[string]string{}
		for _, pair := range positional[1:] {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || k == "" {
				return "", fail(1, "Invalid env pair: %s", pair), true
			}
			if opts["--encoded"] != "" {
				decoded, err := base64.StdEncoding.DecodeString(v)
				if err != nil {
					return "", fail(1, "Invalid base64 value for %s", k), true
				}
				v = string(decoded)
			}
			values[k] = v
		}
		if len(values) == 0 {
			return "", fail(1, "At least one env pair must be given"), true
		}
		for k, v := range values {
			a.config[k] = v
		}
	case "config:unset":
		if len(opts) > 1 || (len(opts) == 1 && opts["--no-restart"] == "") {
			return "", nil, false
		}
		if !exists {
			return "", missingApp(name), true
		}
		if len(positional) == 1 {
			return "", fail(1, "At least one key must be given"), true
		}
		for _, k := range positional[1:] {
			delete(a.config, k)
		}
	case "config:import":
		if opts["--format"] != "json" || opts["--encoded"] != "" || len(positional) != 2 || positional[1] != "-" {
			return "", nil, false
		}
		if !exists {
			return "", missingApp(name), true
		}
		values := map[string]string{}
		if err := json.Unmarshal(stdin, &values); err != nil {
			return "", fail(1, "Invalid json input: %v", err), true
		}
		if len(values) == 0 {
			return "", fail(1, "No config vars to set"), true
		}
		if opts["--replace"] != "" {
			a.config = map[string]string{}
		}
		for k, v := range values {
			a.config[k] = v
		}
	}
	return "", nil, true
}

func (s *Server) domainsCommand(sub string, rest []string) (string, *failure, bool) {
	if sub == "domains:report" {
		switch {
		case len(rest) == 2 && rest[0] == "--global" && rest[1] == "--domains-global-vhosts":
			return strings.Join(s.globalDomains, " "), nil, true
		case len(rest) == 2 && rest[1] == "--domains-app-vhosts" && !strings.HasPrefix(rest[0], "--"):
			a, ok := s.apps[rest[0]]
			if !ok {
				return "", missingApp(rest[0]), true
			}
			return strings.Join(a.domains, " "), nil, true
		}
		return "", nil, false
	}
	for _, arg := range rest {
		if strings.HasPrefix(arg, "--") {
			return "", nil, false
		}
	}
	verb, global := strings.CutSuffix(strings.TrimPrefix(sub, "domains:"), "-global")
	list := &s.globalDomains
	if !global {
		if len(rest) == 0 {
			return "", nil, false
		}
		a, ok := s.apps[rest[0]]
		if !ok {
			return "", missingApp(rest[0]), true
		}
		list, rest = &a.domains, rest[1:]
	}
	switch verb {
	case "add":
		if len(rest) == 0 {
			return "", fail(1, "Please specify a domain name"), true
		}
		*list = uniqueAppend(*list, rest...)
	case "remove":
		if len(rest) == 0 {
			return "", fail(1, "Please specify a domain name"), true
		}
		*list = without(*list, rest...)
	case "set":
		if len(rest) == 0 {
			return "", fail(1, "Please specify a domain name"), true
		}
		*list = uniqueAppend(nil, rest...)
	case "clear":
		if len(rest) != 0 {
			return "", nil, false
		}
		*list = nil
	}
	return "", nil, true
}

func without(list []string, drop ...string) []string {
	var out []string
	for _, v := range list {
		if !contains(drop, v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) portsCommand(sub string, rest []string) (string, *failure, bool) {
	if sub == "ports:report" {
		if len(rest) != 2 || rest[1] != "--ports-map-json" || strings.HasPrefix(rest[0], "--") {
			return "", nil, false
		}
		a, ok := s.apps[rest[0]]
		if !ok {
			return "", missingApp(rest[0]), true
		}
		if len(a.ports) == 0 {
			return "null", nil, true
		}
		type entry struct {
			ContainerPort int    `json:"container_port"`
			HostPort      int    `json:"host_port"`
			Scheme        string `json:"scheme"`
		}
		entries := make([]entry, 0, len(a.ports))
		for _, p := range a.ports {
			entries = append(entries, entry{ContainerPort: p.Container, HostPort: p.Host, Scheme: p.Scheme})
		}
		body, _ := json.Marshal(entries)
		return string(body), nil, true
	}
	if len(rest) == 0 {
		return "", nil, false
	}
	for _, arg := range rest {
		if strings.HasPrefix(arg, "--") {
			return "", nil, false
		}
	}
	a, ok := s.apps[rest[0]]
	if !ok {
		return "", missingApp(rest[0]), true
	}
	args := rest[1:]
	if sub == "ports:clear" {
		if len(args) != 0 {
			return "", nil, false
		}
		a.ports = nil
		return "", nil, true
	}
	if len(args) == 0 {
		return "", fail(1, "No port mappings specified"), true
	}
	if sub == "ports:remove" {
		var kept []PortMapping
		for _, p := range a.ports {
			if !contains(args, p.String()) && !contains(args, strconv.Itoa(p.Host)) {
				kept = append(kept, p)
			}
		}
		a.ports = kept
		return "", nil, true
	}
	mappings := make([]PortMapping, 0, len(args))
	for _, arg := range args {
		p, ok := parsePortMapping(arg)
		if !ok {
			return "", fail(1, "Invalid port mapping %s", arg), true
		}
		mappings = append(mappings, p)
	}
	if sub == "ports:set" {
		a.ports = nil
	}
	for _, p := range mappings {
		found := false
		for _, have := range a.ports {
			if have == p {
				found = true
			}
		}
		if !found {
			a.ports = append(a.ports, p)
		}
	}
	return "", nil, true
}

func parsePortMapping(arg string) (PortMapping, bool) {
	parts := strings.Split(arg, ":")
	if len(parts) != 3 {
		return PortMapping{}, false
	}
	host, herr := strconv.Atoi(parts[1])
	container, cerr := strconv.Atoi(parts[2])
	if herr != nil || cerr != nil {
		return PortMapping{}, false
	}
	return PortMapping{Scheme: parts[0], Host: host, Container: container}, true
}

func (s *Server) buildpacksCommand(sub string, rest []string) (string, *failure, bool) {
	opts, positional, ok := flags(rest, []string{"--index"}, []string{"--replace"})
	if !ok || len(positional) == 0 {
		return "", nil, false
	}
	a, exists := s.apps[positional[0]]
	if !exists {
		return "", missingApp(positional[0]), true
	}
	urls := positional[1:]
	index := 0
	if raw, ok := opts["--index"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return "", fail(1, "Invalid index %s", raw), true
		}
		index = n
	}
	switch sub {
	case "buildpacks:list":
		if len(opts) != 0 || len(urls) != 0 {
			return "", nil, false
		}
		return strings.Join(a.buildpacks, "\n"), nil, true
	case "buildpacks:clear":
		if len(opts) != 0 || len(urls) != 0 {
			return "", nil, false
		}
		a.buildpacks = nil
	case "buildpacks:remove":
		if len(opts) != 0 || len(urls) != 1 {
			return "", nil, false
		}
		if !contains(a.buildpacks, urls[0]) {
			return "", fail(1, "Buildpack %s not found", urls[0]), true
		}
		a.buildpacks = without(a.buildpacks, urls[0])
	case "buildpacks:add":
		if opts["--replace"] != "" || len(urls) != 1 {
			return "", nil, false
		}
		if contains(a.buildpacks, urls[0]) {
			return "", nil, true
		}
		a.buildpacks = insertAt(a.buildpacks, index, urls[0])
	case "buildpacks:set":
		if len(urls) == 0 {
			return "", fail(1, "Please specify a buildpack"), true
		}
		if opts["--replace"] != "" {
			a.buildpacks = uniqueAppend(nil, urls...)
			return "", nil, true
		}
		if len(urls) != 1 {
			return "", nil, false
		}
		if index == 0 {
			index = 1
		}
		if index > len(a.buildpacks) {
			a.buildpacks = uniqueAppend(a.buildpacks, urls[0])
		} else {
			a.buildpacks[index-1] = urls[0]
		}
	}
	return "", nil, true
}

func insertAt(list []string, index int, value string) []string {
	if index < 1 || index > len(list) {
		return append(list, value)
	}
	out := append([]string(nil), list[:index-1]...)
	out = append(out, value)
	return append(out, list[index-1:]...)
}

func (s *Server) psScale(rest []string, quiet bool) (string, *failure, bool) {
	opts, positional, ok := flags(rest, nil, []string{"--replace", "--clear", "--skip-deploy"})
	if !ok || len(positional) == 0 || (opts["--replace"] != "" && opts["--clear"] != "") {
		return "", nil, false
	}
	name := positional[0]
	a, exists := s.apps[name]
	if !exists {
		return "", missingApp(name), true
	}
	tuples := positional[1:]
	if len(opts) == 0 && len(tuples) == 0 {
		var lines []string
		if !quiet {
			lines = append(lines, "-----> Scaling for "+name, "proctype: qty", "--------: ---")
		}
		for _, proc := range sortedKeys(a.scale) {
			lines = append(lines, fmt.Sprintf("%-5s %d", proc+":", a.scale[proc]))
		}
		return strings.Join(lines, "\n"), nil, true
	}
	if opts["--clear"] != "" {
		if len(tuples) != 0 {
			return "", nil, false
		}
		for proc := range a.scale {
			a.scale[proc] = 0
		}
		a.scale["web"] = 1
		return "", nil, true
	}
	scale := map[string]int{}
	for _, tuple := range tuples {
		proc, raw, ok := strings.Cut(tuple, "=")
		qty, err := strconv.Atoi(raw)
		if !ok || proc == "" || err != nil || qty < 0 {
			return "", fail(1, "Invalid process tuple %s", tuple), true
		}
		scale[proc] = qty
	}
	if opts["--replace"] != "" {
		for proc := range a.scale {
			a.scale[proc] = 0
		}
	}
	for proc, qty := range scale {
		a.scale[proc] = qty
	}
	return "", nil, true
}

// reportCommand answers `<plugin>:report <app>|--global --format json` and the
// flag form `<plugin>:report <app> --<plugin>-<property>`.
func (s *Server) reportCommand(p *PropertyPlugin, rest []string) (string, *failure, bool) {
	if len(rest) == 0 {
		return "", nil, false
	}
	target := rest[0]
	global := target == "--global"
	var appProps map[string]string
	if !global {
		if strings.HasPrefix(target, "--") {
			return "", nil, false
		}
		a, ok := s.apps[target]
		if !ok {
			return "", missingApp(target), true
		}
		appProps = a.props[p.Plugin]
	}
	globalProps := s.globalProps[p.Plugin]

	if len(rest) == 3 && rest[1] == "--format" && rest[2] == "json" {
		payload := map[string]string{}
		known := map[string]bool{}
		for _, prop := range p.Properties {
			known[prop.Name] = true
			if prop.GlobalKey != "" {
				payload[prop.GlobalKey] = globalProps[prop.Name]
			}
			if !global && prop.AppKey != "" {
				payload[prop.AppKey] = appProps[prop.Name]
			}
		}
		for name, value := range globalProps {
			if !known[name] {
				payload["global-"+name] = value
			}
		}
		for name, value := range appProps {
			if !known[name] && !global {
				payload[name] = value
			}
		}
		if p.Plugin == "buildpacks" && !global {
			payload["list"] = strings.Join(s.apps[target].buildpacks, ",")
		}
		body, _ := json.Marshal(payload)
		return string(body), nil, true
	}

	if len(rest) == 2 && !global {
		name, ok := strings.CutPrefix(rest[1], "--"+p.Plugin+"-")
		if !ok {
			return "", nil, false
		}
		for _, prop := range p.Properties {
			if prop.Name != name || prop.AppKey == "" {
				continue
			}
			if v := appProps[name]; v != "" {
				return v, nil, true
			}
			if v := globalProps[name]; v != "" {
				return v, nil, true
			}
			return propertyDefaults[p.Plugin][name], nil, true
		}
	}
	return "", nil, false
}

// setPropertyCommand answers `<subcommand> <app>|--global <property> [value]`,
// which sets the property, or unsets it when no value is given.
func (s *Server) setPropertyCommand(p *PropertyPlugin, rest []string) (string, *failure, bool) {
	if len(rest) != 2 && len(rest) != 3 {
		return "", nil, false
	}
	target, name := rest[0], rest[1]
	value := ""
	if len(rest) == 3 {
		value = rest[2]
	}
	if strings.HasPrefix(name, "--") || (strings.HasPrefix(target, "--") && target != "--global") {
		return "", nil, false
	}
	global := target == "--global"
	for _, prop := range p.Properties {
		if prop.Name != name {
			continue
		}
		if global && prop.GlobalKey == "" {
			return "", fail(1, "Property %s cannot be set globally", name), true
		}
		if !global && prop.AppKey == "" {
			return "", fail(1, "Property %s can only be set globally", name), true
		}
	}
	if global {
		setProp(s.globalProps, p.Plugin, name, value)
		return "", nil, true
	}
	a, ok := s.apps[target]
	if !ok {
		return "", missingApp(target), true
	}
	setProp(a.props, p.Plugin, name, value)
	return "", nil, true
}

// serviceFlags are the create flags a datastore takes, and the info key each
// one is reported under. --image and --image-version make up "version", and
// the passwords are never reported.
var serviceFlags = map[string]string{
	"--config-options":      "config-options",
	"--custom-env":          "custom-env",
	"--memory":              "memory",
	"--shm-size":            "shm-size",
	"--initial-network":     "initial-network",
	"--post-create-network": "post-create-network",
	"--post-start-network":  "post-start-network",
}

// serviceInfoKeys are the keys a new service reports, empty until set.
var serviceInfoKeys = []string{
	"config-options", "custom-env", "memory", "shm-size", "initial-network",
	"post-create-network", "post-start-network", "exposed-ports", "status",
	"version", "dsn", "links",
}

func (s *Server) serviceCommand(serviceType, command string, rest []string) (string, *failure, bool) {
	services := s.services[serviceType]
	if command == "list" {
		if len(rest) != 0 {
			return "", nil, false
		}
		return strings.Join(sortedKeys(services), "\n"), nil, true
	}
	if len(rest) == 0 || strings.HasPrefix(rest[0], "--") {
		return "", nil, false
	}
	name := rest[0]
	svc := services[name]
	if command != "create" && svc == nil {
		switch command {
		case "exists", "destroy", "info", "links", "linked", "link", "unlink":
			return "", fail(1, "service %s does not exist", name), true
		}
		return "", nil, false
	}

	switch command {
	case "exists":
		if len(rest) != 1 {
			return "", nil, false
		}
	case "create":
		valued := []string{"--image", "--image-version", "--password", "--root-password"}
		for flag := range serviceFlags {
			valued = append(valued, flag)
		}
		opts, positional, ok := flags(rest[1:], valued, nil)
		if !ok || len(positional) != 0 {
			return "", nil, false
		}
		if svc != nil {
			return "", fail(1, "%s service %s already exists", serviceType, name), true
		}
		s.newService(serviceType, name, opts)
	case "destroy":
		if len(rest) != 1 {
			return "", nil, false
		}
		if len(svc.links) > 0 {
			return "", fail(1, "Cannot delete linked service"), true
		}
		delete(services, name)
	case "info":
		if len(rest) == 3 && rest[1] == "--format" && rest[2] == "json" {
			body, _ := json.Marshal(s.serviceInfo(svc))
			return string(body), nil, true
		}
		if len(rest) == 2 {
			info := s.serviceInfo(svc)
			if value, ok := info[strings.TrimPrefix(rest[1], "--")]; ok && strings.HasPrefix(rest[1], "--") {
				return value, nil, true
			}
		}
		return "", nil, false
	case "links":
		if len(rest) != 1 {
			return "", nil, false
		}
		return strings.Join(sortedKeys(svc.links), "\n"), nil, true
	case "linked", "link", "unlink":
		if len(rest) != 2 {
			return "", nil, false
		}
		appName := rest[1]
		a, ok := s.apps[appName]
		if !ok {
			return "", missingApp(appName), true
		}
		linked := svc.links[appName]
		switch command {
		case "linked":
			if !linked {
				return "", fail(1, "Service %s is not linked to %s", name, appName), true
			}
		case "link":
			if linked {
				return "", fail(1, "Already linked as %s", linkKey(serviceType)), true
			}
			s.link(serviceType, svc, appName, a)
		case "unlink":
			if !linked {
				return "", fail(1, "Not linked to app %s", appName), true
			}
			delete(svc.links, appName)
			body := dsnBody(svc.info["dsn"])
			for k, v := range a.config {
				if body != "" && strings.Contains(v, body) {
					delete(a.config, k)
				}
			}
		}
	default:
		return "", nil, false
	}
	return "", nil, true
}

// newService creates a service from its create flags. The caller holds s.mu.
func (s *Server) newService(serviceType, name string, opts map[string]string) *service {
	info := map[string]string{}
	for _, key := range serviceInfoKeys {
		info[key] = ""
	}
	image, version := splitImage(s.datastores[serviceType])
	if v := opts["--image"]; v != "" {
		image = v
	}
	if v := opts["--image-version"]; v != "" {
		version = v
	}
	info["version"] = image
	if version != "" {
		info["version"] = image + ":" + version
	}
	for flag, key := range serviceFlags {
		if v, ok := opts[flag]; ok {
			info[key] = v
		}
	}
	sum := sha256.Sum256([]byte(serviceType + "/" + name))
	password := hex.EncodeToString(sum[:])[:32]
	info["dsn"] = fmt.Sprintf("%s://%s:%s@dokku-%s-%s:%d/%s", serviceType, serviceType, password, serviceType, name, servicePort(serviceType), strings.ReplaceAll(name, "-", "_"))
	info["exposed-ports"] = "-"
	info["status"] = "running"
	svc := &service{info: info, links: map[string]bool{}}
	if s.services[serviceType] == nil {
		s.services[serviceType] = map[string]*service{}
	}
	s.services[serviceType][name] = svc
	return svc
}

// serviceInfo is what `<service>:info` reports for svc.
func (s *Server) serviceInfo(svc *service) map[string]string {
	info := copyMap(svc.info)
	info["links"] = strings.Join(sortedKeys(svc.links), ",")
	return info
}

// link links svc to an app and writes its DSN into the app's config. The
// caller holds s.mu.
func (s *Server) link(serviceType string, svc *service, appName string, a *app) {
	svc.links[appName] = true
	a.config[linkKey(serviceType)] = svc.info["dsn"]
}

// linkKey is the config key a link writes, as the datastore plugins name it.
func linkKey(serviceType string) string {
	switch serviceType {
	case "postgres", "mysql", "mariadb":
		return "DATABASE_URL"
	}
	return strings.ToUpper(strings.ReplaceAll(serviceType, "-", "_")) + "_URL"
}

func servicePort(serviceType string) int {
	switch serviceType {
	case "postgres":
		return 5432
	case "mysql", "mariadb":
		return 3306
	case "redis":
		return 6379
	case "mongo":
		return 27017
	}
	return 1
}

// dsnBody is a DSN without its scheme, the part a link's config value carries.
func dsnBody(dsn string) string {
	if _, body, ok := strings.Cut(dsn, "://"); ok {
		return body
	}
	return dsn
}

// splitImage splits an image reference into its name and tag.
func splitImage(ref string) (string, string) {
	colon := strings.LastIndex(ref, ":")
	if colon < 0 || colon < strings.LastIndex(ref, "/") {
		return ref, ""
	}
	return ref[:colon], ref[colon+1:]
}
