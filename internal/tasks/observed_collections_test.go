package tasks

func init() {
	registerObservedFixtures(observedCollectionFixtures)
}

// observedCollectionFixtures covers the toggle, resource, scheduler-k3s pair
// and other collection-shaped tasks.
func observedCollectionFixtures() map[string]observedFixture {
	app := exportAppFrom
	global := exportGlobalFrom

	return map[string]observedFixture{
		"dokku_checks_toggle": {
			responses: map[string]string{
				"checks:report web --format json": `{"disabled-list":"_all_","skipped-list":"none"}`,
				"checks:report on --format json":  `{"disabled-list":"none","skipped-list":"none"}`,
			},
			export: app(ChecksToggleTask{}),
			cases: []observedCase{
				{name: "disabled, planned present", task: ChecksToggleTask{App: "web", State: StatePresent}, want: ChecksToggleTask{App: "web", State: StateAbsent}},
				{name: "enabled, planned absent", task: ChecksToggleTask{App: "on", State: StateAbsent}, want: ChecksToggleTask{App: "on", State: StatePresent}, exists: true},
			},
		},
		"dokku_domains_toggle": {
			responses: map[string]string{
				"--quiet domains:report web --domains-app-enabled": "false",
				"--quiet domains:report on --domains-app-enabled":  "true",
			},
			export: app(DomainsToggleTask{}),
			cases: []observedCase{
				{name: "disabled, planned present", task: DomainsToggleTask{App: "web", State: StatePresent}, want: DomainsToggleTask{App: "web", State: StateAbsent}},
				{name: "enabled", task: DomainsToggleTask{App: "on", State: StatePresent}, want: DomainsToggleTask{App: "on", State: StatePresent}, exists: true},
			},
		},
		"dokku_proxy_toggle": {
			responses: map[string]string{
				"--quiet proxy:report web --proxy-enabled": "false",
				"--quiet proxy:report on --proxy-enabled":  "true",
			},
			export: app(ProxyToggleTask{}),
			cases: []observedCase{
				{name: "disabled, planned present", task: ProxyToggleTask{App: "web", State: StatePresent}, want: ProxyToggleTask{App: "web", State: StateAbsent}},
				{name: "enabled, planned absent", task: ProxyToggleTask{App: "on", State: StateAbsent}, want: ProxyToggleTask{App: "on", State: StatePresent}, exists: true},
			},
		},
		"dokku_maintenance": {
			responses: map[string]string{
				"maintenance:report web --format json": `{"enabled":"true"}`,
				"maintenance:report off --format json": `{"enabled":"false"}`,
			},
			export: app(MaintenanceTask{}),
			cases: []observedCase{
				{name: "off, planned present", task: MaintenanceTask{App: "off", State: StatePresent}, want: MaintenanceTask{App: "off", State: StateAbsent}},
				{name: "on, planned absent", task: MaintenanceTask{App: "web", State: StateAbsent}, want: MaintenanceTask{App: "web", State: StatePresent}, exists: true},
			},
		},
		"dokku_resource_limit": {
			responses: map[string]string{
				"--quiet resource:report web --format json":        `{"_default_.limit.cpu":"100","_default_.limit.memory":"512","_default_.reserve.memory":"256","web.limit.cpu":"","resource-_default_.limit.cpu":"100"}`,
				"--quiet resource:limit web":                       "cpu: 100\nmemory: 512\nnvidia-gpu: \nnetwork: 0",
				"--quiet resource:limit --process-type worker web": "cpu: \nmemory: \nnvidia-gpu: \nnetwork: ",
			},
			export: app(ResourceLimitTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared resources",
					task:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "200"}, State: StatePresent},
					want:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "100"}, State: StatePresent},
					exists: true,
				},
				{
					name:   "clear_before reads every resource",
					task:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "100"}, ClearBefore: boolPtr(true), State: StatePresent},
					want:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "100", "memory": "512"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent on a process type with nothing set",
					task: ResourceLimitTask{App: "web", ProcessType: "worker", State: StateAbsent},
					want: ResourceLimitTask{App: "web", ProcessType: "worker", State: StateAbsent},
				},
				{
					name:   "set reads every resource",
					task:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "100"}, State: StateSet},
					want:   ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "100", "memory": "512"}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on a process type with nothing set",
					task: ResourceLimitTask{App: "web", ProcessType: "worker", State: StateClear},
					want: ResourceLimitTask{App: "web", ProcessType: "worker", State: StateClear},
				},
			},
		},
		"dokku_resource_reserve": {
			responses: map[string]string{
				"--quiet resource:report web --format json":          `{"_default_.reserve.memory":"256","_default_.limit.cpu":"100"}`,
				"--quiet resource:reserve web":                       "cpu: \nmemory: 256\nnvidia-gpu: \nnetwork: ",
				"--quiet resource:reserve --process-type worker web": "cpu: \nmemory: 0",
			},
			export: app(ResourceReserveTask{}),
			cases: []observedCase{
				{
					name: "absent on a process type with nothing set",
					task: ResourceReserveTask{App: "web", ProcessType: "worker", State: StateAbsent},
					want: ResourceReserveTask{App: "web", ProcessType: "worker", State: StateAbsent},
				},
				{
					name:   "absent with something set reads it all",
					task:   ResourceReserveTask{App: "web", State: StateAbsent},
					want:   ResourceReserveTask{App: "web", Resources: map[string]string{"memory": "256"}, State: StatePresent},
					exists: true,
				},
				{
					name:   "clear with something set reads it as set",
					task:   ResourceReserveTask{App: "web", State: StateClear},
					want:   ResourceReserveTask{App: "web", Resources: map[string]string{"memory": "256"}, State: StateSet},
					exists: true,
				},
			},
		},
		"dokku_scheduler_k3s_labels": {
			responses: map[string]string{
				"--quiet scheduler-k3s:labels:report web --format json":                                                 `{"web.deployment.tier":"edge","web.deployment.app.kubernetes.io/part-of":"shop"}`,
				"--quiet scheduler-k3s:labels:report web --resource-type deployment --process-type web --format json":   `{"web.deployment.tier":"edge","web.deployment.app.kubernetes.io/part-of":"shop"}`,
				"--quiet scheduler-k3s:labels:report web --resource-type ingress --process-type --global --format json": `{}`,
			},
			export: app(SchedulerK3sLabelsTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared keys",
					task:   SchedulerK3sLabelsTask{App: "web", ProcessType: "web", ResourceType: "deployment", Labels: map[string]string{"tier": "core", "missing": "x"}, State: StatePresent},
					want:   SchedulerK3sLabelsTask{App: "web", ProcessType: "web", ResourceType: "deployment", Labels: map[string]string{"tier": "edge"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: SchedulerK3sLabelsTask{App: "web", ProcessType: "web", ResourceType: "deployment", Labels: map[string]string{"missing": ""}, State: StateAbsent},
					want: SchedulerK3sLabelsTask{App: "web", ProcessType: "web", ResourceType: "deployment", State: StateAbsent},
				},
				{
					name: "clear on an empty scope",
					task: SchedulerK3sLabelsTask{App: "web", ResourceType: "ingress", State: StateClear},
					want: SchedulerK3sLabelsTask{App: "web", ResourceType: "ingress", State: StateClear},
				},
			},
		},
		"dokku_scheduler_k3s_annotations": {
			responses: map[string]string{
				"--quiet scheduler-k3s:annotations:report web --format json":                                                 `{"global.ingress.nginx.ingress.kubernetes.io/proxy-body-size":"10m"}`,
				"--quiet scheduler-k3s:annotations:report web --resource-type ingress --process-type --global --format json": `{"global.ingress.nginx.ingress.kubernetes.io/proxy-body-size":"10m"}`,
				"--quiet scheduler-k3s:annotations:report web --resource-type service --process-type web --format json":      `{}`,
			},
			export: app(SchedulerK3sAnnotationsTask{}),
			cases: []observedCase{
				{
					name:   "clear reads every key",
					task:   SchedulerK3sAnnotationsTask{App: "web", ResourceType: "ingress", State: StateClear},
					want:   SchedulerK3sAnnotationsTask{App: "web", ResourceType: "ingress", Annotations: map[string]string{"nginx.ingress.kubernetes.io/proxy-body-size": "10m"}, State: StateSet},
					exists: true,
				},
				{
					name: "set on an empty scope",
					task: SchedulerK3sAnnotationsTask{App: "web", ProcessType: "web", ResourceType: "service", Annotations: map[string]string{"a": "b"}, State: StateSet},
					want: SchedulerK3sAnnotationsTask{App: "web", ProcessType: "web", ResourceType: "service", State: StateClear},
				},
			},
		},
		"dokku_scheduler_k3s_chart": {
			responses: map[string]string{
				"--quiet scheduler-k3s:charts:report --format json":               `{"ingress-nginx.controller.replicaCount":"3","traefik.ports.web.port":"8000"}`,
				"--quiet scheduler-k3s:charts:report ingress-nginx --format json": `{"ingress-nginx.controller.replicaCount":"3","traefik.ports.web.port":"8000"}`,
				"--quiet scheduler-k3s:charts:report traefik --format json":       `{"ingress-nginx.controller.replicaCount":"3","traefik.ports.web.port":"8000"}`,
				"--quiet scheduler-k3s:charts:report cert-manager --format json":  `{}`,
			},
			export: global(SchedulerK3sChartTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared values",
					task:   SchedulerK3sChartTask{Chart: "ingress-nginx", Values: map[string]any{"controller": map[string]any{"replicaCount": 2, "other": "x"}}, State: StatePresent},
					want:   SchedulerK3sChartTask{Chart: "ingress-nginx", Values: map[string]any{"controller.replicaCount": "3"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: SchedulerK3sChartTask{Chart: "cert-manager", Values: map[string]any{"installCRDs": "true"}, State: StateAbsent},
					want: SchedulerK3sChartTask{Chart: "cert-manager", State: StateAbsent},
				},
				{
					name:   "set reads every value of the chart",
					task:   SchedulerK3sChartTask{Chart: "ingress-nginx", Values: map[string]any{"controller.service.type": "NodePort"}, State: StateSet},
					want:   SchedulerK3sChartTask{Chart: "ingress-nginx", Values: map[string]any{"controller.replicaCount": "3"}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on a chart with no values",
					task: SchedulerK3sChartTask{Chart: "cert-manager", State: StateClear},
					want: SchedulerK3sChartTask{Chart: "cert-manager", State: StateClear},
				},
			},
		},
		"dokku_scheduler_k3s_node_sysctls": {
			responses: map[string]string{
				"--quiet scheduler-k3s:node-sysctls:report --stored --format json":                 `{"--global":{"vm.max_map_count":"262144"},"gpu":{"net.core.somaxconn":"4096"},"empty":{}}`,
				"--quiet scheduler-k3s:node-sysctls:report --stored --global --format json":        `{"--global":{"vm.max_map_count":"262144"}}`,
				"--quiet scheduler-k3s:node-sysctls:report --stored --profile gpu --format json":   `{"gpu":{"net.core.somaxconn":"4096"}}`,
				"--quiet scheduler-k3s:node-sysctls:report --stored --profile spare --format json": `{"spare":{}}`,
			},
			export: global(SchedulerK3sNodeSysctlsTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared sysctls",
					task:   SchedulerK3sNodeSysctlsTask{Profile: "gpu", Sysctls: map[string]string{"net.core.somaxconn": "1024"}, State: StatePresent},
					want:   SchedulerK3sNodeSysctlsTask{Profile: "gpu", Sysctls: map[string]string{"net.core.somaxconn": "4096"}, State: StatePresent},
					exists: true,
				},
				{
					name: "clear on an empty profile",
					task: SchedulerK3sNodeSysctlsTask{Profile: "spare", State: StateClear},
					want: SchedulerK3sNodeSysctlsTask{Profile: "spare", State: StateClear},
				},
			},
		},
		"dokku_scheduler_k3s_autoscaling_auth": {
			responses: map[string]string{
				"--quiet scheduler-k3s:autoscaling-auth:report web --format json": `{"datadog.apiKey":"k3y","datadog.appKey":"app","datadog.datadogSite":"datadoghq.com"}`,
			},
			export: app(SchedulerK3sAutoscalingAuthTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared keys",
					task:   SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "datadog", Metadata: map[string]string{"apiKey": "other"}, State: StatePresent},
					want:   SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "datadog", Metadata: map[string]string{"apiKey": "k3y"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent trigger",
					task: SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "prometheus", Metadata: map[string]string{"user": ""}, State: StateAbsent},
					want: SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "prometheus", State: StateAbsent},
				},
				{
					name: "clear on an empty trigger",
					task: SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "prometheus", State: StateClear},
					want: SchedulerK3sAutoscalingAuthTask{App: "web", Trigger: "prometheus", State: StateClear},
				},
			},
		},
		"dokku_buildpacks": {
			responses: map[string]string{
				"--quiet buildpacks:report web --format json": `{"list":"https://github.com/heroku/heroku-buildpack-nodejs.git,https://github.com/heroku/heroku-buildpack-ruby.git"}`,
				"--quiet buildpacks:list web":                 "=====> web buildpack urls\nhttps://github.com/heroku/heroku-buildpack-nodejs.git\nhttps://github.com/heroku/heroku-buildpack-ruby.git",
				"--quiet buildpacks:list empty":               "=====> empty buildpack urls",
			},
			export: app(BuildpacksTask{}),
			cases: []observedCase{
				{
					name:   "absent reads only the named buildpacks",
					task:   BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-ruby.git", "https://example.com/missing.git"}, State: StateAbsent},
					want:   BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-ruby.git"}, State: StatePresent},
					exists: true,
				},
				{
					name:   "absent with none named reads every buildpack in order",
					task:   BuildpacksTask{App: "web", State: StateAbsent},
					want:   BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-nodejs.git", "https://github.com/heroku/heroku-buildpack-ruby.git"}, State: StatePresent},
					exists: true,
				},
				{
					name:   "set reads every buildpack in order",
					task:   BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-ruby.git"}, State: StateSet},
					want:   BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-nodejs.git", "https://github.com/heroku/heroku-buildpack-ruby.git"}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on an app with no buildpacks",
					task: BuildpacksTask{App: "empty", State: StateClear},
					want: BuildpacksTask{App: "empty", State: StateClear},
				},
				{
					name: "absent on an app with none",
					task: BuildpacksTask{App: "empty", State: StateAbsent},
					want: BuildpacksTask{App: "empty", State: StateAbsent},
				},
			},
		},
		"dokku_domains": {
			responses: map[string]string{
				"domains:report web --domains-app-vhosts":         "web.example.com www.example.com",
				"domains:report bare --domains-app-vhosts":        "",
				"domains:report --global --domains-global-vhosts": "example.com",
			},
			export: app(DomainsTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared domains",
					task:   DomainsTask{App: "web", Domains: []string{"www.example.com", "new.example.com"}, State: StatePresent},
					want:   DomainsTask{App: "web", Domains: []string{"www.example.com"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: DomainsTask{App: "web", Domains: []string{"gone.example.com"}, State: StateAbsent},
					want: DomainsTask{App: "web", State: StateAbsent},
				},
				{
					name: "clear on an app with none",
					task: DomainsTask{App: "bare", State: StateClear},
					want: DomainsTask{App: "bare", State: StateClear},
				},
				{
					name:   "set reads every global domain",
					task:   DomainsTask{Global: true, Domains: []string{"other.example.com"}, State: StateSet},
					want:   DomainsTask{Global: true, Domains: []string{"example.com"}, State: StateSet},
					exists: true,
				},
			},
		},
		"dokku_ports": {
			responses: map[string]string{
				"--quiet ports:report web --ports-map-json":  `[{"container_port":5000,"host_port":80,"scheme":"http"},{"container_port":5000,"host_port":443,"scheme":"https"}]`,
				"--quiet ports:report bare --ports-map-json": `null`,
			},
			export: app(PortsTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared mappings",
					task:   PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}, {Scheme: "http", Host: 8080, Container: 5000}}, State: StatePresent},
					want:   PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 8080, Container: 5000}}, State: StateAbsent},
					want: PortsTask{App: "web", State: StateAbsent},
				},
				{
					name: "clear on an app with none",
					task: PortsTask{App: "bare", State: StateClear},
					want: PortsTask{App: "bare", State: StateClear},
				},
			},
		},
		"dokku_docker_options": {
			responses: map[string]string{
				"--quiet docker-options:report web --format json": `{"build":"--pull","build-list":["--pull"],"deploy.web":"-v /data:/data","deploy.web-list":["-v /data:/data"],"run-list":[]}`,
			},
			export: app(DockerOptionsTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: DockerOptionsTask{App: "web", Phase: "run", Option: "--init", State: StatePresent},
					want: DockerOptionsTask{App: "web", Phase: "run", Option: "--init", State: StateAbsent},
				},
				{
					name:   "present, planned absent",
					task:   DockerOptionsTask{App: "web", Phase: "deploy", ProcessType: "web", Option: "-v /data:/data", State: StateAbsent},
					want:   DockerOptionsTask{App: "web", Phase: "deploy", ProcessType: "web", Option: "-v /data:/data", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_ps_scale": {
			responses: map[string]string{
				"--quiet ps:scale web":  "-----> Scaling for web\nproctype: qty\n--------: ---\nweb:  2\nworker: 0\nclock: 1",
				"--quiet ps:scale idle": "-----> Scaling for idle\nproctype: qty\n--------: ---\nweb:  0",
			},
			export: app(PsScaleTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared process types",
					task:   PsScaleTask{App: "web", Scale: map[string]int{"web": 3, "release": 0}, State: StatePresent},
					want:   PsScaleTask{App: "web", Scale: map[string]int{"web": 2, "release": 0}, State: StatePresent},
					exists: true,
				},
				{
					name: "nothing running",
					task: PsScaleTask{App: "idle", Scale: map[string]int{"web": 1}, State: StateSet},
					want: PsScaleTask{App: "idle", State: StateClear},
				},
				{
					name:   "clear reads the whole formation",
					task:   PsScaleTask{App: "web", State: StateClear},
					want:   PsScaleTask{App: "web", Scale: map[string]int{"clock": 1, "web": 2}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on an app running nothing",
					task: PsScaleTask{App: "idle", State: StateClear},
					want: PsScaleTask{App: "idle", State: StateClear},
				},
			},
		},
		"dokku_network": {
			responses: map[string]string{
				"--quiet network:list --format json": `[{"Name":"bridge","DokkuManaged":false},{"Name":"backend","DokkuManaged":true}]`,
				"--quiet network:exists backend":     "",
			},
			exitCodes: map[string]int{"--quiet network:exists gone": 1},
			export:    global(NetworkTask{}),
			cases: []observedCase{
				{name: "absent", task: NetworkTask{Name: "gone", State: StatePresent}, want: NetworkTask{Name: "gone", State: StateAbsent}},
				{name: "present, planned absent", task: NetworkTask{Name: "backend", State: StateAbsent}, want: NetworkTask{Name: "backend", State: StatePresent}, exists: true},
			},
		},
		"dokku_letsencrypt": {
			responses: map[string]string{
				"--quiet letsencrypt:active web":   "true",
				"--quiet letsencrypt:active plain": "false",
			},
			export: app(LetsencryptTask{}),
			cases: []observedCase{
				{name: "inactive", task: LetsencryptTask{App: "plain", State: StatePresent}, want: LetsencryptTask{App: "plain", State: StateAbsent}},
				{name: "active, planned absent", task: LetsencryptTask{App: "web", State: StateAbsent}, want: LetsencryptTask{App: "web", State: StatePresent}, exists: true},
			},
		},
		"dokku_scheduler_k3s_profile": {
			responses: map[string]string{
				"--quiet scheduler-k3s:profiles:list --format json": `[{"name":"gpu","role":"worker","kubelet_args":["max-pods=50","node-labels=gpu=true"],"taint_scheduling":true,"allow_unknown_hosts":true}]`,
			},
			export: global(SchedulerK3sProfileTask{}),
			cases: []observedCase{
				{
					name:   "present reads the stored definition",
					task:   SchedulerK3sProfileTask{Name: "gpu", Role: "server", State: StatePresent},
					want:   SchedulerK3sProfileTask{Name: "gpu", Role: "worker", KubeletArgs: []string{"max-pods=50", "node-labels=gpu=true"}, TaintScheduling: true, AllowUnknownHosts: true, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: SchedulerK3sProfileTask{Name: "spare", Role: "worker", State: StateAbsent},
					want: SchedulerK3sProfileTask{Name: "spare", State: StateAbsent},
				},
			},
		},
	}
}
