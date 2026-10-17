package tasks

func init() {
	registerObservedFixtures(observedServiceFixtures)
}

// observedServiceServer is a server with two datastore services: postgres
// my-db, exposed, linked to web, backed up and created with options, and
// redis cache, which carries none of that. Every service task's fixture
// starts from it, so the exporters that enumerate every service see both.
func observedServiceServer() map[string]string {
	return mergeFixtures(
		map[string]string{
			"--quiet plugin:trigger service-list":         "postgres:my-db\nredis:cache",
			"--quiet postgres:exists my-db":               "",
			"--quiet redis:exists cache":                  "",
			"--quiet apps:exists web":                     "",
			"--quiet apps:exists api":                     "",
			"--quiet postgres:links my-db":                "web",
			"--quiet redis:links cache":                   "",
			"--quiet postgres:linked my-db web":           "",
			"--quiet postgres:info my-db --exposed-ports": "5432->5433",
			"--quiet redis:info cache --exposed-ports":    "",
		},
		infoFixture("postgres", "my-db", map[string]string{
			"version":                              "postgres:17.2",
			"memory":                               "512",
			"custom-env":                           "LANG=C;TZ=UTC",
			"config-options":                       "--max-connections=200",
			"shm-size":                             "256m",
			"initial-network":                      "bridge",
			"post-create-network":                  "backend,metrics",
			"post-start-network":                   "frontend",
			"backup-schedule":                      "0 3 * * *",
			"backup-bucket":                        "my-bucket",
			"backup-use-iam":                       "false",
			"backup-access-key-id-fingerprint":     backupFingerprint("AKIAEXAMPLE"),
			"backup-secret-access-key-fingerprint": backupFingerprint("secret"),
			"backup-encryption-fingerprint":        backupFingerprint("passphrase"),
		}),
		infoFixture("redis", "cache", map[string]string{"version": "redis:7.2.5"}),
	)
}

// observedHttpAuthServer is app web with http-auth serving one user, one
// allowed IP and two auth domains, and app off with http-auth disabled and
// nothing configured.
func observedHttpAuthServer() map[string]string {
	return map[string]string{
		"http-auth:report web --format json": `{"enabled":"true","users":"admin","allowed-ips":"10.0.0.1","domains":"b.example.com a.example.com"}`,
		"--quiet http-auth:export-users web": "admin:" + exportHttpAuthHash + "\n",
		"http-auth:report off --format json": `{"enabled":"false","users":"","allowed-ips":"","domains":""}`,
		"--quiet http-auth:export-users off": "",
	}
}

// observedStorageMountServer is app web with three mounts in the default
// scope - a named entry, a legacy host-path bind mount, and an entry subpath
// carrying a chown and a volume option - and one in the worker scope whose rw
// option the mounts list cannot express, so export emits it as a single mount.
func observedStorageMountServer() map[string]string {
	return map[string]string{
		"--quiet storage:report web --format json": storageReport(
			reportAttachment{EntryName: "data", ContainerPath: "/app/data"},
			reportAttachment{EntryName: "legacy-abc123", HostPath: "/var/lib/dokku/data/storage/web", ContainerPath: "/app/storage", Readonly: true, Phases: []string{"run"}},
			reportAttachment{EntryName: "data", ContainerPath: "/app/uploads", Subpath: "uploads", VolumeChown: "herokuish", VolumeOptions: "Z"},
			reportAttachment{EntryName: "data", ContainerPath: "/app/cache", ProcessType: "worker", VolumeOptions: "rw"},
		),
	}
}

// observedServiceFixtures covers the datastore service, http-auth and storage
// tasks.
func observedServiceFixtures() map[string]observedFixture {
	return map[string]observedFixture{
		"dokku_service_create": {
			responses: observedServiceServer(),
			exitCodes: map[string]int{"--quiet postgres:exists gone": 1},
			export:    exportGlobalFrom(ServiceCreateTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: ServiceCreateTask{Service: "postgres", Name: "gone", State: StatePresent},
					want: ServiceCreateTask{Service: "postgres", Name: "gone", State: StateAbsent},
				},
				{
					name:   "an unpinned task reads every option",
					task:   ServiceCreateTask{Service: "postgres", Name: "my-db", State: StatePresent},
					exists: true,
					want: ServiceCreateTask{
						Service:           "postgres",
						Name:              "my-db",
						Image:             "postgres",
						ImageVersion:      "17.2",
						Memory:            512,
						CustomEnv:         map[string]string{"LANG": "C", "TZ": "UTC"},
						ConfigOptions:     "--max-connections=200",
						ShmSize:           "256m",
						InitialNetwork:    "bridge",
						PostCreateNetwork: []string{"backend", "metrics"},
						PostStartNetwork:  []string{"frontend"},
						State:             StatePresent,
					},
				},
				{
					name:   "present, planned absent",
					task:   ServiceCreateTask{Service: "redis", Name: "cache", State: StateAbsent},
					want:   ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", ImageVersion: "7.2.5", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_service_link": {
			responses: observedServiceServer(),
			exitCodes: map[string]int{"--quiet postgres:linked my-db api": 1},
			export:    exportAppFrom(ServiceLinkTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: ServiceLinkTask{App: "api", Service: "postgres", Name: "my-db", State: StatePresent},
					want: ServiceLinkTask{App: "api", Service: "postgres", Name: "my-db", State: StateAbsent},
				},
				{
					name:   "present, planned absent",
					task:   ServiceLinkTask{App: "web", Service: "postgres", Name: "my-db", State: StateAbsent},
					want:   ServiceLinkTask{App: "web", Service: "postgres", Name: "my-db", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_service_expose": {
			responses: observedServiceServer(),
			export:    exportGlobalFrom(ServiceExposeTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: ServiceExposeTask{Service: "redis", Name: "cache", Ports: []string{"6379"}, State: StatePresent},
					want: ServiceExposeTask{Service: "redis", Name: "cache", State: StateAbsent},
				},
				{
					name:   "present, planned on other ports",
					task:   ServiceExposeTask{Service: "postgres", Name: "my-db", Ports: []string{"9999"}, State: StatePresent},
					want:   ServiceExposeTask{Service: "postgres", Name: "my-db", Ports: []string{"5433"}, State: StatePresent},
					exists: true,
				},
				{
					name:   "set reads the exposure as set",
					task:   ServiceExposeTask{Service: "postgres", Name: "my-db", Ports: []string{"9999"}, State: StateSet},
					want:   ServiceExposeTask{Service: "postgres", Name: "my-db", Ports: []string{"5433"}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on an unexposed service",
					task: ServiceExposeTask{Service: "redis", Name: "cache", State: StateClear},
					want: ServiceExposeTask{Service: "redis", Name: "cache", State: StateClear},
				},
			},
		},
		"dokku_service_backup": {
			// cache stores IAM auth, a region, a signature version and an
			// endpoint but no schedule, so export skips it while a read
			// observes those settings; spare stores nothing.
			responses: mergeFixtures(
				observedServiceServer(),
				infoFixture("redis", "cache", map[string]string{
					"version":                  "redis:7.2.5",
					"backup-use-iam":           "true",
					"backup-default-region":    "us-east-1",
					"backup-signature-version": "s3v4",
					"backup-endpoint-url":      "https://s3.example.com",
				}),
				infoFixture("redis", "spare", map[string]string{"version": "redis:7.2.5"}),
				map[string]string{"--quiet redis:exists spare": ""},
			),
			export: exportGlobalFrom(ServiceBackupTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: ServiceBackupTask{Service: "redis", Name: "spare", Schedule: "0 1 * * *", Bucket: "b", State: StatePresent},
					want: ServiceBackupTask{Service: "redis", Name: "spare", State: StateAbsent},
				},
				{
					name: "auth settings without a schedule",
					task: ServiceBackupTask{Service: "redis", Name: "cache", State: StateAbsent},
					want: ServiceBackupTask{
						Service:             "redis",
						Name:                "cache",
						UseIam:              true,
						AwsDefaultRegion:    "us-east-1",
						AwsSignatureVersion: "s3v4",
						EndpointURL:         "https://s3.example.com",
						State:               StatePresent,
					},
					exists: true,
				},
				{
					name:   "present, planned absent",
					task:   ServiceBackupTask{Service: "postgres", Name: "my-db", State: StateAbsent},
					want:   ServiceBackupTask{Service: "postgres", Name: "my-db", Schedule: "0 3 * * *", Bucket: "my-bucket", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_http_auth": {
			responses: observedHttpAuthServer(),
			export:    exportAppFrom(HttpAuthTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: HttpAuthTask{App: "off", State: StatePresent},
					want: HttpAuthTask{App: "off", State: StateAbsent},
				},
				{
					name:   "present, planned absent",
					task:   HttpAuthTask{App: "web", State: StateAbsent},
					want:   HttpAuthTask{App: "web", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_http_auth_user": {
			responses: observedHttpAuthServer(),
			export:    exportAppFrom(HttpAuthUserTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "ghost", Password: "pw"}}, State: StatePresent},
					want: HttpAuthUserTask{App: "web", State: StateAbsent},
				},
				{
					name:   "a password user reads its stored hash",
					task:   HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Password: "pw"}}, State: StatePresent},
					want:   HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Hash: exportHttpAuthHash}}, State: StatePresent},
					exists: true,
				},
				{
					name:   "absent with no users owns the list",
					task:   HttpAuthUserTask{App: "web", State: StateAbsent},
					want:   HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Hash: exportHttpAuthHash}}, State: StateSet},
					exists: true,
				},
				{
					name:   "clear owns the list",
					task:   HttpAuthUserTask{App: "web", State: StateClear},
					want:   HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Hash: exportHttpAuthHash}}, State: StateSet},
					exists: true,
				},
				{
					name: "clear on an app with no users",
					task: HttpAuthUserTask{App: "off", State: StateClear},
					want: HttpAuthUserTask{App: "off", State: StateClear},
				},
			},
		},
		"dokku_http_auth_domain": {
			responses: observedHttpAuthServer(),
			export:    exportAppFrom(HttpAuthDomainTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: HttpAuthDomainTask{App: "web", Domains: []string{"c.example.com"}, State: StatePresent},
					want: HttpAuthDomainTask{App: "web", State: StateAbsent},
				},
				{
					name:   "present reads only the declared domains",
					task:   HttpAuthDomainTask{App: "web", Domains: []string{"a.example.com", "c.example.com"}, State: StatePresent},
					want:   HttpAuthDomainTask{App: "web", Domains: []string{"a.example.com"}, State: StatePresent},
					exists: true,
				},
				{
					name: "clear on an app with none",
					task: HttpAuthDomainTask{App: "off", State: StateClear},
					want: HttpAuthDomainTask{App: "off", State: StateClear},
				},
			},
		},
		"dokku_http_auth_allowed_ip": {
			responses: observedHttpAuthServer(),
			export:    exportAppFrom(HttpAuthAllowedIpTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: HttpAuthAllowedIpTask{App: "web", AllowedIps: []string{"10.0.0.9"}, State: StateAbsent},
					want: HttpAuthAllowedIpTask{App: "web", State: StateAbsent},
				},
				{
					name:   "clear owns the list",
					task:   HttpAuthAllowedIpTask{App: "web", State: StateClear},
					want:   HttpAuthAllowedIpTask{App: "web", AllowedIps: []string{"10.0.0.1"}, State: StateSet},
					exists: true,
				},
			},
		},
		"dokku_storage_entry": {
			responses: storageEntriesFixture(),
			export:    exportGlobalFrom(StorageEntryTask{}),
			cases: []observedCase{
				{
					name: "absent",
					task: StorageEntryTask{Name: "gone", State: StatePresent},
					want: StorageEntryTask{Name: "gone", State: StateAbsent},
				},
				{
					name:   "present, planned absent",
					task:   StorageEntryTask{Name: "app-data", State: StateAbsent},
					want:   StorageEntryTask{Name: "app-data", Path: "/var/lib/dokku/data/storage/app-data", Scheduler: "docker-local", Chown: "herokuish", Mode: "0777", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_storage_mount": {
			responses: observedStorageMountServer(),
			export:    exportAppFrom(StorageMountTask{}),
			cases: []observedCase{
				{
					name: "absent single mount",
					task: StorageMountTask{App: "web", EntryName: "data", ContainerDir: "/app/missing", State: StateAbsent},
					want: StorageMountTask{App: "web", ContainerDir: "/app/missing", State: StateAbsent},
				},
				{
					name:   "a single mount reads whatever holds its path",
					task:   StorageMountTask{App: "web", EntryName: "other", ContainerDir: "/app/storage", State: StatePresent},
					want:   StorageMountTask{App: "web", HostDir: "/var/lib/dokku/data/storage/web", ContainerDir: "/app/storage", Phases: []string{"run"}, Readonly: true, State: StatePresent},
					exists: true,
				},
				{
					name:   "a single mount reads its subpath, chown and options",
					task:   StorageMountTask{App: "web", EntryName: "data", ContainerDir: "/app/uploads", State: StatePresent},
					want:   StorageMountTask{App: "web", EntryName: "data", ContainerDir: "/app/uploads", Subpath: "uploads", VolumeChown: "herokuish", VolumeOptions: "Z", State: StatePresent},
					exists: true,
				},
				{
					name:   "a present list reads only the declared paths",
					task:   StorageMountTask{App: "web", Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/app/data"}, {EntryName: "data", ContainerDir: "/app/new"}}, State: StatePresent},
					want:   StorageMountTask{App: "web", Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/app/data"}}, State: StatePresent},
					exists: true,
				},
				{
					name: "clear on an empty scope",
					task: StorageMountTask{App: "web", ProcessType: "release", State: StateClear},
					want: StorageMountTask{App: "web", ProcessType: "release", State: StateClear},
				},
			},
		},
	}
}
