// Package fakedokku is the stateful fake dokku server behind sdk/dokkutest.
//
// It lives under internal/ so docket's own engine tests, which sit inside
// package tasks and so cannot import anything that imports it, can drive it
// too. It depends on internal/subprocess alone. Callers outside the module use
// it through sdk/dokkutest, which fills in the property tables from the
// published catalog; see that package for how to use it.
package fakedokku

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// DefaultVersion is the dokku version a new Server reports: the oldest one
// docket's integration tests run against.
const DefaultVersion = "0.38.31"

// Call is one command a Server was sent.
type Call struct {
	// Command is the program, "dokku" for every command a task sends.
	Command string

	// Args are the command's arguments as they would have run, unmasked.
	Args []string

	// Stdin is what the command read on standard input, if anything.
	Stdin string

	// Unknown is true for a command the Server does not model and no handler
	// answered.
	Unknown bool
}

// Line returns the call's arguments joined by spaces, the form Count and
// Handle take.
func (c Call) Line() string {
	return strings.Join(c.Args, " ")
}

// Handler answers one command in place of the Server's own model. It has the
// shape of the runner sdk.ContextWithRunner installs.
type Handler func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error)

// Property is one property of a property plugin, and the keys its report
// carries it under in each scope. An empty key means the property has no form
// in that scope.
type Property struct {
	Name      string
	AppKey    string
	GlobalKey string
}

// PropertyPlugin is a dokku plugin whose properties a Server answers for: its
// `<plugin>:report` reads them and its set subcommand writes them.
type PropertyPlugin struct {
	// Plugin is the report command's prefix, for example "nginx".
	Plugin string

	// Subcommand sets and unsets a property, for example "nginx:set" or
	// "buildpacks:set-property".
	Subcommand string

	// Properties are the properties the report always carries, empty when
	// unset. A property set under any other name is carried once it is set,
	// the way dokku reports a dynamic family such as letsencrypt's
	// dns-provider-*.
	Properties []Property
}

// PortMapping is one port mapping, as `ports:report --ports-map-json` reports
// it.
type PortMapping struct {
	Scheme    string
	Host      int
	Container int
}

// String renders the mapping as the scheme:host:container argument the ports
// subcommands take.
func (p PortMapping) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Scheme, p.Host, p.Container)
}

// Server is a fake dokku that holds state, answers the commands docket's tasks
// send in the formats they parse, and applies their changes to that state. It
// records every call, and it fails any command it does not model: the call
// returns an error naming it and, when the Server was made with a testing.TB,
// the test fails.
//
// A Server is safe for concurrent use.
type Server struct {
	t testing.TB

	mu            sync.Mutex
	version       string
	plugins       map[string]string
	datastores    map[string]string
	apps          map[string]*app
	globalDomains []string
	propPlugins   map[string]*PropertyPlugin
	propBySub     map[string]*PropertyPlugin
	globalProps   map[string]map[string]string
	services      map[string]map[string]*service
	handlers      []handlerEntry
	fallback      Handler
	calls         []Call
}

// app is the state a Server holds for one app.
type app struct {
	config     map[string]string
	domains    []string
	ports      []PortMapping
	buildpacks []string
	scale      map[string]int
	props      map[string]map[string]string
}

func newApp() *app {
	return &app{config: map[string]string{}, scale: map[string]int{}, props: map[string]map[string]string{}}
}

// service is the state a Server holds for one datastore service.
type service struct {
	info  map[string]string
	links map[string]bool
}

type handlerEntry struct {
	line   string
	prefix bool
	h      Handler
}

// New returns an empty Server: dokku at DefaultVersion with its core plugins,
// no apps and no services. t, when not nil, fails the test on a command the
// Server does not model. properties are the property plugins it answers for.
func New(t testing.TB, properties []PropertyPlugin) *Server {
	s := &Server{
		t:           t,
		version:     DefaultVersion,
		plugins:     map[string]string{},
		datastores:  map[string]string{},
		apps:        map[string]*app{},
		propPlugins: map[string]*PropertyPlugin{},
		propBySub:   map[string]*PropertyPlugin{},
		globalProps: map[string]map[string]string{},
		services:    map[string]map[string]*service{},
	}
	for i := range properties {
		p := properties[i]
		p.Properties = append([]Property(nil), p.Properties...)
		s.propPlugins[p.Plugin] = &p
		s.propBySub[p.Subcommand] = &p
	}
	return s
}

// Context returns a copy of ctx whose commands go to the Server.
func (s *Server) Context(ctx context.Context) context.Context {
	return subprocess.ContextWithRunner(ctx, s.Run)
}

// Run answers one command. It is the runner Context installs, exported so a
// test can wrap it.
func (s *Server) Run(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	var stdin []byte
	if in.Stdin != nil {
		read, err := io.ReadAll(in.Stdin)
		if err != nil {
			return subprocess.ExecCommandResponse{}, fmt.Errorf("dokkutest: read stdin: %w", err)
		}
		stdin = read
		in.Stdin = bytes.NewReader(stdin)
	}
	call := Call{Command: in.Command, Args: append([]string(nil), in.Args...), Stdin: string(stdin)}
	line := call.Line()

	s.mu.Lock()
	var handler Handler
	for i := len(s.handlers) - 1; i >= 0; i-- {
		entry := s.handlers[i]
		if line == entry.line || (entry.prefix && strings.HasPrefix(line, entry.line)) {
			handler = entry.h
			break
		}
	}
	if handler != nil {
		s.calls = append(s.calls, call)
		s.mu.Unlock()
		return handler(ctx, in)
	}

	var out string
	var fail *failure
	known := false
	if in.Command == "dokku" {
		out, fail, known = s.dispatch(in.Args, stdin)
	}
	fallback := s.fallback
	call.Unknown = !known && fallback == nil
	s.calls = append(s.calls, call)
	s.mu.Unlock()

	if !known {
		if fallback != nil {
			return fallback(ctx, in)
		}
		msg := fmt.Sprintf("dokkutest: unknown command: %s %s", in.Command, line)
		if s.t != nil {
			s.t.Errorf("%s", msg)
		}
		return subprocess.ExecCommandResponse{}, errors.New(msg)
	}
	command := strings.TrimSpace(in.Command + " " + line)
	if fail != nil {
		return exitResponse(command, fail.code, out, fail.stderr)
	}
	return subprocess.ExecCommandResponse{Command: command, Stdout: out}, nil
}

// Calls returns every command the Server was sent, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Lines returns the arguments of every command the Server was sent, each
// joined by spaces, in order.
func (s *Server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := make([]string, 0, len(s.calls))
	for _, call := range s.calls {
		lines = append(lines, call.Line())
	}
	return lines
}

// Count returns how many commands the Server was sent whose arguments, joined
// by spaces, are line.
func (s *Server) Count(line string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, call := range s.calls {
		if call.Line() == line {
			n++
		}
	}
	return n
}

// Unknown returns the commands the Server was sent that it does not model and
// no handler answered, each joined by spaces.
func (s *Server) Unknown() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var lines []string
	for _, call := range s.calls {
		if call.Unknown {
			lines = append(lines, call.Line())
		}
	}
	return lines
}

// ResetCalls forgets every recorded call, leaving the state alone.
func (s *Server) ResetCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

// Handle answers every command whose arguments, joined by spaces, are line
// with h instead of the Server's model, which neither reads nor changes the
// state. A later handler for the same command wins.
func (s *Server) Handle(line string, h Handler) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handlerEntry{line: line, h: h})
	return s
}

// HandlePrefix is Handle for every command whose joined arguments start with
// prefix.
func (s *Server) HandlePrefix(prefix string, h Handler) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handlerEntry{line: prefix, prefix: true, h: h})
	return s
}

// Fallback answers every command the Server does not model with h instead of
// failing it. Calls h answers are not reported by Unknown.
func (s *Server) Fallback(h Handler) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallback = h
	return s
}

// Respond is a Handler that succeeds and prints stdout.
func Respond(stdout string) Handler {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Command: strings.TrimSpace(in.Command + " " + strings.Join(in.Args, " ")), Stdout: stdout}, nil
	}
}

// Exit is a Handler for a command that ran and exited with code, printing
// stderr, the way the real runner reports it: the response carries the exit
// code, and the error is an *ExecError with Ran set.
func Exit(code int, stderr string) Handler {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return exitResponse(strings.TrimSpace(in.Command+" "+strings.Join(in.Args, " ")), code, "", stderr)
	}
}

// Unavailable is a Handler for a command that could not run at all, such as a
// dokku binary that is not installed. A probe reports it as an error rather
// than as the resource being absent.
func Unavailable(err error) Handler {
	return func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{}, err
	}
}

func exitResponse(command string, code int, stdout, stderr string) (subprocess.ExecCommandResponse, error) {
	res := subprocess.ExecCommandResponse{Command: command, Stdout: stdout, Stderr: stderr, ExitCode: code}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", code)
	}
	return res, &subprocess.ExecError{Response: res, Err: errors.New(msg), Ran: true}
}

// SeedVersion sets the dokku version the Server reports.
func (s *Server) SeedVersion(version string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = version
	return s
}

// SeedPlugin installs a plugin at version, alongside dokku's core plugins.
func (s *Server) SeedPlugin(name, version string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plugins[name] = version
	return s
}

// SeedDatastore installs a datastore plugin, such as postgres, so the Server
// answers its service commands. A service created without an image runs
// defaultImage, a reference such as "postgres:17.2".
func (s *Server) SeedDatastore(name, defaultImage string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seedDatastore(name, defaultImage)
	return s
}

func (s *Server) seedDatastore(name, defaultImage string) {
	if _, ok := s.plugins[name]; !ok {
		s.plugins[name] = "2.4.0"
	}
	s.datastores[name] = defaultImage
	if s.services[name] == nil {
		s.services[name] = map[string]*service{}
	}
}

// SeedApp creates each named app that does not exist yet.
func (s *Server) SeedApp(names ...string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range names {
		s.app(name)
	}
	return s
}

// app returns the named app, creating it if needed. The caller holds s.mu.
func (s *Server) app(name string) *app {
	a, ok := s.apps[name]
	if !ok {
		a = newApp()
		s.apps[name] = a
	}
	return a
}

// SeedConfig sets config values on an app, creating the app if needed.
func (s *Server) SeedConfig(appName string, values map[string]string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.app(appName)
	for k, v := range values {
		a.config[k] = v
	}
	return s
}

// SeedDomains replaces an app's domains, creating the app if needed.
func (s *Server) SeedDomains(appName string, domains ...string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app(appName).domains = uniqueAppend(nil, domains...)
	return s
}

// SeedGlobalDomains replaces the global domains.
func (s *Server) SeedGlobalDomains(domains ...string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalDomains = uniqueAppend(nil, domains...)
	return s
}

// SeedPorts replaces an app's port mappings, creating the app if needed.
func (s *Server) SeedPorts(appName string, mappings ...PortMapping) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app(appName).ports = append([]PortMapping(nil), mappings...)
	return s
}

// SeedBuildpacks replaces an app's buildpacks, in precedence order, creating
// the app if needed.
func (s *Server) SeedBuildpacks(appName string, urls ...string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app(appName).buildpacks = uniqueAppend(nil, urls...)
	return s
}

// SeedScale sets process quantities on an app, creating the app if needed. A
// process type at zero is still reported, the way dokku reports one scaled
// down.
func (s *Server) SeedScale(appName string, scale map[string]int) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.app(appName)
	for proc, qty := range scale {
		a.scale[proc] = qty
	}
	return s
}

// SeedProperty sets a plugin property on an app, creating the app if needed.
// An empty value leaves it unset.
func (s *Server) SeedProperty(plugin, appName, property, value string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	setProp(s.app(appName).props, plugin, property, value)
	return s
}

// SeedGlobalProperty sets a plugin property globally. An empty value leaves it
// unset.
func (s *Server) SeedGlobalProperty(plugin, property, value string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	setProp(s.globalProps, plugin, property, value)
	return s
}

// SeedService creates a datastore service, installing its plugin if needed.
// info sets keys `<service>:info` reports, over the defaults a create writes:
// "version" is the running image, "dsn" the connection string a link writes
// into an app's config.
func (s *Server) SeedService(serviceType, name string, info map[string]string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.datastores[serviceType]; !ok {
		s.seedDatastore(serviceType, serviceType+":latest")
	}
	svc := s.newService(serviceType, name, nil)
	for k, v := range info {
		svc.info[k] = v
	}
	return s
}

// SeedLink links a service to an app, as `<service>:link` does: the app's
// config gains the service's DSN. The service and the app must exist.
func (s *Server) SeedLink(serviceType, name, appName string) *Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc := s.services[serviceType][name]
	a := s.apps[appName]
	if svc == nil || a == nil {
		panic(fmt.Sprintf("dokkutest: SeedLink(%q, %q, %q): seed the service and the app first", serviceType, name, appName))
	}
	s.link(serviceType, svc, appName, a)
	return s
}

// Apps returns the names of the apps that exist, sorted.
func (s *Server) Apps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedKeys(s.apps)
}

// HasApp reports whether an app exists.
func (s *Server) HasApp(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.apps[name]
	return ok
}

// Config returns a copy of an app's config, or nil when the app does not
// exist.
func (s *Server) Config(appName string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[appName]
	if !ok {
		return nil
	}
	return copyMap(a.config)
}

// Domains returns an app's domains in the order they were added.
func (s *Server) Domains(appName string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.apps[appName]; ok {
		return append([]string(nil), a.domains...)
	}
	return nil
}

// GlobalDomains returns the global domains in the order they were added.
func (s *Server) GlobalDomains() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.globalDomains...)
}

// Ports returns an app's port mappings in the order they were added.
func (s *Server) Ports(appName string) []PortMapping {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.apps[appName]; ok {
		return append([]PortMapping(nil), a.ports...)
	}
	return nil
}

// Buildpacks returns an app's buildpacks in precedence order.
func (s *Server) Buildpacks(appName string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.apps[appName]; ok {
		return append([]string(nil), a.buildpacks...)
	}
	return nil
}

// Scale returns a copy of an app's process quantities, zeroes included.
func (s *Server) Scale(appName string) map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.apps[appName]
	if !ok {
		return nil
	}
	out := make(map[string]int, len(a.scale))
	for k, v := range a.scale {
		out[k] = v
	}
	return out
}

// Property returns a plugin property's value on an app and whether it is set.
func (s *Server) Property(plugin, appName, property string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.apps[appName]; ok {
		v, ok := a.props[plugin][property]
		return v, ok
	}
	return "", false
}

// GlobalProperty returns a plugin property's global value and whether it is
// set.
func (s *Server) GlobalProperty(plugin, property string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.globalProps[plugin][property]
	return v, ok
}

// Services returns the names of the services of a datastore type, sorted.
func (s *Server) Services(serviceType string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedKeys(s.services[serviceType])
}

// ServiceInfo returns a copy of what `<service>:info` reports for a service,
// or nil when it does not exist.
func (s *Server) ServiceInfo(serviceType, name string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc := s.services[serviceType][name]
	if svc == nil {
		return nil
	}
	return copyMap(svc.info)
}

// Links returns the apps a service is linked to, sorted.
func (s *Server) Links(serviceType, name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc := s.services[serviceType][name]
	if svc == nil {
		return nil
	}
	return sortedKeys(svc.links)
}

func setProp(props map[string]map[string]string, plugin, property, value string) {
	if value == "" {
		delete(props[plugin], property)
		return
	}
	if props[plugin] == nil {
		props[plugin] = map[string]string{}
	}
	props[plugin][property] = value
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// uniqueAppend appends each value list does not hold yet.
func uniqueAppend(list []string, values ...string) []string {
	for _, v := range values {
		found := false
		for _, have := range list {
			if have == v {
				found = true
				break
			}
		}
		if !found {
			list = append(list, v)
		}
	}
	return list
}
