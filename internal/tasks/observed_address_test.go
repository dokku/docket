package tasks

import "testing"

// observedAddressCases are ReadAddress reads against the fixtures in the
// other observed_*_test.go files, by task type: an absent resource for every
// task, and a present one for each task whose exported bodies do not already
// read one back. observedFixtures merges them into the fixture of the same
// type. Property tasks state theirs in propertyTaskFixture instead.
func observedAddressCases() map[string][]observedAddressCase {
	absent := func(address string, want Task) observedAddressCase {
		return observedAddressCase{address: address, want: want}
	}
	present := func(address string, want Task) observedAddressCase {
		return observedAddressCase{address: address, want: want, exists: true}
	}

	return map[string][]observedAddressCase{
		"dokku_acl_app":     {absent("dokku_acl_app[app=empty]", AclAppTask{App: "empty", State: StateClear})},
		"dokku_acl_service": {absent("dokku_acl_service[service=empty,type=redis]", AclServiceTask{Service: "empty", Type: "redis", State: StateClear})},
		"dokku_app":         {absent("dokku_app[app=gone]", AppTask{App: "gone", State: StateAbsent})},
		"dokku_app_clone": {
			absent("dokku_app_clone[app=gone]", AppCloneTask{App: "gone", State: StateAbsent}),
			present("dokku_app_clone[app=web]", AppCloneTask{App: "web", State: StatePresent}),
		},
		"dokku_app_lock":   {absent("dokku_app_lock[app=open]", AppLockTask{App: "open", State: StateAbsent})},
		"dokku_buildpacks": {absent("dokku_buildpacks[app=empty]", BuildpacksTask{App: "empty", State: StateAbsent})},
		"dokku_certs":      {absent("dokku_certs[app=gone]", CertsTask{App: "gone", State: StateAbsent})},
		"dokku_checks_toggle": {
			absent("dokku_checks_toggle[app=web]", ChecksToggleTask{App: "web", State: StateAbsent}),
			present("dokku_checks_toggle[app=on]", ChecksToggleTask{App: "on", State: StatePresent}),
		},
		"dokku_config":         {absent("dokku_config[app=empty]", ConfigTask{App: "empty", State: StateClear})},
		"dokku_docker_options": {absent("dokku_docker_options[app=web,phase=run,option=--init]", DockerOptionsTask{App: "web", Phase: "run", Option: "--init", State: StateAbsent})},
		"dokku_domains":        {absent("dokku_domains[app=bare]", DomainsTask{App: "bare", State: StateClear})},
		"dokku_domains_toggle": {
			absent("dokku_domains_toggle[app=web]", DomainsToggleTask{App: "web", State: StateAbsent}),
			present("dokku_domains_toggle[app=on]", DomainsToggleTask{App: "on", State: StatePresent}),
		},
		"dokku_git_auth": {
			absent("dokku_git_auth[host=example.com]", GitAuthTask{Host: "example.com", State: StateAbsent}),
			present("dokku_git_auth[host=github.com]", GitAuthTask{Host: "github.com", State: StatePresent}),
		},
		"dokku_git_from_archive":     {absent("dokku_git_from_archive[app=other]", GitFromArchiveTask{App: "other", State: StateAbsent})},
		"dokku_git_from_image":       {absent("dokku_git_from_image[app=other]", GitFromImageTask{App: "other", State: StateAbsent})},
		"dokku_git_sync":             {absent("dokku_git_sync[app=other]", GitSyncTask{App: "other", State: StateAbsent})},
		"dokku_http_auth":            {absent("dokku_http_auth[app=off]", HttpAuthTask{App: "off", State: StateAbsent})},
		"dokku_http_auth_allowed_ip": {absent("dokku_http_auth_allowed_ip[app=off]", HttpAuthAllowedIpTask{App: "off", State: StateClear})},
		"dokku_http_auth_domain":     {absent("dokku_http_auth_domain[app=off]", HttpAuthDomainTask{App: "off", State: StateClear})},
		"dokku_http_auth_user": {
			absent("dokku_http_auth_user[app=off]", HttpAuthUserTask{App: "off", State: StateAbsent}),
			present("dokku_http_auth_user[app=web]", HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Hash: exportHttpAuthHash}}, State: StateSet}),
		},
		"dokku_letsencrypt":             {absent("dokku_letsencrypt[app=plain]", LetsencryptTask{App: "plain", State: StateAbsent})},
		"dokku_maintenance":             {absent("dokku_maintenance[app=off]", MaintenanceTask{App: "off", State: StateAbsent})},
		"dokku_maintenance_custom_page": {absent("dokku_maintenance_custom_page[app=gone]", MaintenanceCustomPageTask{App: "gone", State: StateAbsent})},
		"dokku_network":                 {absent("dokku_network[name=gone]", NetworkTask{Name: "gone", State: StateAbsent})},
		"dokku_plugin":                  {absent("dokku_plugin[name=postgres]", PluginTask{Name: "postgres", State: StateAbsent})},
		"dokku_ports":                   {absent("dokku_ports[app=bare]", PortsTask{App: "bare", State: StateClear})},
		"dokku_proxy_toggle": {
			absent("dokku_proxy_toggle[app=web]", ProxyToggleTask{App: "web", State: StateAbsent}),
			present("dokku_proxy_toggle[app=on]", ProxyToggleTask{App: "on", State: StatePresent}),
		},
		"dokku_ps_scale": {
			absent("dokku_ps_scale[app=idle]", PsScaleTask{App: "idle", State: StateSet}),
			present("dokku_ps_scale[app=web]", PsScaleTask{App: "web", Scale: map[string]int{"clock": 1, "web": 2}, State: StateSet}),
		},
		"dokku_registry_auth": {
			absent("dokku_registry_auth[app=web,server=docker.io]", RegistryAuthTask{App: "web", Server: "docker.io", State: StateAbsent}),
			present("dokku_registry_auth[app=web,server=quay.io]", RegistryAuthTask{App: "web", Server: "quay.io", State: StatePresent}),
		},
		"dokku_resource_limit":   {absent("dokku_resource_limit[app=web,process_type=worker]", ResourceLimitTask{App: "web", ProcessType: "worker", State: StateAbsent})},
		"dokku_resource_reserve": {absent("dokku_resource_reserve[app=web,process_type=worker]", ResourceReserveTask{App: "web", ProcessType: "worker", State: StateAbsent})},
		"dokku_scheduler_k3s_annotations": {absent("dokku_scheduler_k3s_annotations[app=web,process_type=web,resource_type=service]",
			SchedulerK3sAnnotationsTask{App: "web", ProcessType: "web", ResourceType: "service", State: StateClear})},
		"dokku_scheduler_k3s_autoscaling_auth": {absent("dokku_scheduler_k3s_autoscaling_auth[app=web,trigger=prometheus]",
			SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "prometheus", State: StateClear})},
		"dokku_scheduler_k3s_chart":        {absent("dokku_scheduler_k3s_chart[chart=cert-manager]", SchedulerK3sChartTask{Chart: "cert-manager", State: StateAbsent})},
		"dokku_scheduler_k3s_labels":       {absent("dokku_scheduler_k3s_labels[app=web,resource_type=ingress]", SchedulerK3sLabelsTask{App: "web", ResourceType: "ingress", State: StateClear})},
		"dokku_scheduler_k3s_node_sysctls": {absent("dokku_scheduler_k3s_node_sysctls[profile=spare]", SchedulerK3sNodeSysctlsTask{Profile: "spare", State: StateClear})},
		"dokku_scheduler_k3s_profile":      {absent("dokku_scheduler_k3s_profile[name=spare]", SchedulerK3sProfileTask{Name: "spare", State: StateAbsent})},
		"dokku_service_backup":             {absent("dokku_service_backup[service=redis,name=spare]", ServiceBackupTask{Service: "redis", Name: "spare", State: StateAbsent})},
		"dokku_service_create":             {absent("dokku_service_create[service=postgres,name=gone]", ServiceCreateTask{Service: "postgres", Name: "gone", State: StateAbsent})},
		"dokku_service_expose":             {absent("dokku_service_expose[service=redis,name=cache]", ServiceExposeTask{Service: "redis", Name: "cache", State: StateAbsent})},
		"dokku_service_link":               {absent("dokku_service_link[app=api,service=postgres,name=my-db]", ServiceLinkTask{App: "api", Service: "postgres", Name: "my-db", State: StateAbsent})},
		"dokku_service_property":           {absent("dokku_service_property[service=postgres,name=db,property=log-driver]", ServicePropertyTask{Service: "postgres", Name: "db", Property: "log-driver", State: StateAbsent})},
		"dokku_ssh_key":                    {absent("dokku_ssh_key[name=deploy-bot]", SshKeyTask{Name: "deploy-bot", State: StateAbsent})},
		"dokku_storage_entry":              {absent("dokku_storage_entry[name=gone]", StorageEntryTask{Name: "gone", State: StateAbsent})},
		"dokku_storage_mount": {
			absent("dokku_storage_mount[app=web,container_dir=/app/missing]", StorageMountTask{App: "web", ContainerDir: "/app/missing", State: StateAbsent}),
			absent("dokku_storage_mount[app=web,process_type=release]", StorageMountTask{App: "web", ProcessType: "release", State: StateClear}),
		},
	}
}

// TestObservedAddressCasesNameFixtures keeps observedAddressCases from naming
// a task with no fixture to read against, where its cases would never run.
func TestObservedAddressCasesNameFixtures(t *testing.T) {
	t.Parallel()
	fixtures := map[string]bool{}
	for _, set := range observedFixtureSets {
		for name := range set() {
			fixtures[name] = true
		}
	}
	for name := range observedAddressCases() {
		if !fixtures[name] {
			t.Errorf("observedAddressCases names %q, which has no observed fixture", name)
		}
	}
}
