package tasks

// An export narrowed with --app used to skip the global play outright, so the
// recipe it wrote named services, networks and storage entries it never
// created, and failed on the fresh server a gradual migration applies it to
// (#671). The global play of such an export now carries what the selected
// apps depend on, and nothing else: the server-wide defaults - global
// properties, ssh keys, the global cert and registry login, the k3s profiles
// and charts - are not any one app's to move.

// networkAttachProperties are the dokku_network_property properties whose
// value names networks. network:set stores a list of them joined by commas,
// the same form splitNetworks reads a service's networks back from.
var networkAttachProperties = map[string]bool{
	"attach-post-create": true,
	"attach-post-deploy": true,
	"initial-network":    true,
}

// dependencyServiceTypes are the global task types that describe a datastore
// service, emitted in full for every service an app links to.
var dependencyServiceTypes = map[string]bool{
	"dokku_service_create":   true,
	"dokku_service_property": true,
	"dokku_service_expose":   true,
	"dokku_service_backup":   true,
	"dokku_acl_service":      true,
}

// serviceRef names one datastore service instance by type and name.
type serviceRef struct {
	service string
	name    string
}

// exportDependencies is the set of global resources the exported apps
// depend on. A nil set depends on nothing in particular, which is what every
// export but an --app one uses: it wants every type and keeps every body.
type exportDependencies struct {
	plugins  map[string]bool
	networks map[string]bool
	entries  map[string]bool
	services map[serviceRef]bool
}

// newExportDependencies collects the dependencies of every task in the app
// plays given. The bodies have already been through processBody, which never
// templates the fields an edge is read from.
func newExportDependencies(plays []ExportedPlay) *exportDependencies {
	deps := &exportDependencies{
		plugins:  map[string]bool{},
		networks: map[string]bool{},
		entries:  map[string]bool{},
		services: map[serviceRef]bool{},
	}
	for _, play := range plays {
		for _, task := range play.Tasks {
			deps.addTask(task.Body)
		}
	}
	return deps
}

// empty reports whether the apps depend on no global resource at all, in
// which case the global play is not read.
func (d *exportDependencies) empty() bool {
	return len(d.plugins) == 0 && len(d.networks) == 0 && len(d.entries) == 0 && len(d.services) == 0
}

// addTask records the global resources body depends on. The plugins and the
// services come from TaskRequires, the same derivation the catalog publishes
// for each example; the networks and storage entries are named by values no
// object declaration covers, so they are read off the fields themselves.
func (d *exportDependencies) addTask(body interface{}) {
	task, ok := body.(Task)
	if !ok {
		return
	}
	for _, required := range TaskRequires(task) {
		switch r := required.(type) {
		case *PluginTask:
			d.plugins[r.Name] = true
		case *ServiceCreateTask:
			d.services[serviceRef{service: r.Service, name: r.Name}] = true
		}
	}
	switch b := body.(type) {
	case NetworkPropertyTask:
		if networkAttachProperties[b.Property] {
			d.addNetworks(splitNetworks(b.Value)...)
		}
	case ServiceCreateTask:
		d.addNetworks(b.InitialNetwork)
		d.addNetworks(b.PostCreateNetwork...)
		d.addNetworks(b.PostStartNetwork...)
	case StorageMountTask:
		d.addEntry(b.EntryName)
		for _, mount := range b.Mounts {
			d.addEntry(mount.EntryName)
		}
	}
}

// addNetworks records the named networks, skipping empty names.
func (d *exportDependencies) addNetworks(names ...string) {
	for _, name := range names {
		if name != "" {
			d.networks[name] = true
		}
	}
}

// addEntry records a storage entry name. A legacy host_dir mount names none.
func (d *exportDependencies) addEntry(name string) {
	if name != "" {
		d.entries[name] = true
	}
}

// wantsType reports whether any dependency could be a resource of typeKey, so
// an exporter nothing needs is never run. A service pulls in networks and
// plugins of its own that the apps may not name, so either is read whenever
// a service is.
func (d *exportDependencies) wantsType(typeKey string) bool {
	if d == nil {
		return true
	}
	hasServices := len(d.services) > 0
	switch {
	case dependencyServiceTypes[typeKey]:
		return hasServices
	case typeKey == "dokku_plugin":
		return hasServices || len(d.plugins) > 0
	case typeKey == "dokku_network":
		return hasServices || len(d.networks) > 0
	case typeKey == "dokku_storage_entry":
		return len(d.entries) > 0
	}
	return false
}

// keeps reports whether body is one of the resources depended on. A body
// that is not the value form of its task is kept, so appendBodies reports it
// the way an unrestricted export would rather than it vanishing here.
func (d *exportDependencies) keeps(typeKey string, body interface{}) bool {
	if d == nil {
		return true
	}
	if exportedBodyType(typeKey, body) != nil {
		return true
	}
	switch b := body.(type) {
	case PluginTask:
		return d.plugins[b.Name]
	case NetworkTask:
		return d.networks[b.Name]
	case StorageEntryTask:
		return d.entries[b.Name]
	case ServiceCreateTask:
		return d.services[serviceRef{service: b.Service, name: b.Name}]
	case ServicePropertyTask:
		return d.services[serviceRef{service: b.Service, name: b.Name}]
	case ServiceExposeTask:
		return d.services[serviceRef{service: b.Service, name: b.Name}]
	case ServiceBackupTask:
		return d.services[serviceRef{service: b.Service, name: b.Name}]
	case AclServiceTask:
		// dokku_acl_service names the instance in Service and the datastore
		// in Type, the other way round from the rest of the family.
		return d.services[serviceRef{service: b.Type, name: b.Service}]
	}
	return false
}
