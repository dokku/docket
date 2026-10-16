package tasks

import (
	"archive/tar"
	"bytes"
	"context"
)

func init() {
	registerObservedFixtures(observedGitFixtures)
}

// observedCert is the certificate the dokku_certs fixture has installed.
var observedCert = certPEM("observed-cert")

// observedPageTarball is the archive maintenance:custom-page-export streams for
// a page whose maintenance.html is content.
func observedPageTarball(content string) string {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "maintenance.html", Mode: 0o644, Size: int64(len(content))})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	return buf.String()
}

// observedPageChecksum is the custom-page-sha256 maintenance:report carries for
// a page whose maintenance.html is content.
func observedPageChecksum(content string) string {
	tarBytes, err := buildMaintenancePageTarball(content)
	if err != nil {
		panic(err)
	}
	sum, err := maintenanceTarballChecksum(tarBytes)
	if err != nil {
		panic(err)
	}
	return sum
}

// observedGitFixtures covers the git deploy-source tasks, the app clone and
// lock tasks, and the plugin, ssh key, ACL, registry auth, certs and custom
// maintenance page tasks.
func observedGitFixtures() map[string]observedFixture {
	page := "<html>down for maintenance</html>"

	return map[string]observedFixture{
		"dokku_git_sync": {
			responses: map[string]string{
				"apps:report web --format json":   `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123"}`,
				"git:report web --format json":    `{"deploy-branch":"main"}`,
				"apps:report other --format json": `{"app-deploy-source":"dockerfile","app-deploy-source-metadata":""}`,
			},
			export: exportAppFrom(GitSyncTask{}),
			cases: []observedCase{
				{
					name:   "a different ref reads the synced one",
					task:   GitSyncTask{App: "web", Remote: "https://github.com/org/repo.git", GitRef: "develop", State: StatePresent},
					want:   GitSyncTask{App: "web", Remote: "https://github.com/org/repo.git", GitRef: "main", State: StatePresent},
					exists: true,
				},
				{
					name: "an app not deployed by git:sync",
					task: GitSyncTask{App: "other", Remote: "https://github.com/org/repo.git", State: StatePresent},
					want: GitSyncTask{App: "other", State: StateAbsent},
				},
			},
		},
		"dokku_git_from_image": {
			responses: map[string]string{
				"apps:report web --format json":   `{"app-deploy-source":"docker-image","app-deploy-source-metadata":"registry.example.com/org/web:1.2.3"}`,
				"apps:report other --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123"}`,
			},
			export: exportAppFrom(GitFromImageTask{}),
			cases: []observedCase{
				{
					name:   "a different image reads the deployed one",
					task:   GitFromImageTask{App: "web", Image: "registry.example.com/org/web:2.0.0", State: StateDeployed},
					want:   GitFromImageTask{App: "web", Image: "registry.example.com/org/web:1.2.3", State: StateDeployed},
					exists: true,
				},
				{
					name: "an app not deployed from an image",
					task: GitFromImageTask{App: "other", Image: "registry.example.com/org/web:1.2.3", State: StateDeployed},
					want: GitFromImageTask{App: "other", State: StateAbsent},
				},
			},
		},
		"dokku_git_from_archive": {
			responses: map[string]string{
				"apps:report web --format json":   `{"app-deploy-source":"tar.gz","app-deploy-source-metadata":"https://example.com/web.tar.gz"}`,
				"apps:report other --format json": `{"app-deploy-source":"docker-image","app-deploy-source-metadata":"web:1"}`,
			},
			export: exportAppFrom(GitFromArchiveTask{}),
			cases: []observedCase{
				{
					name:   "a different archive type reads the deployed one",
					task:   GitFromArchiveTask{App: "web", ArchiveURL: "https://example.com/web.tar.gz", ArchiveType: "zip", State: StateDeployed},
					want:   GitFromArchiveTask{App: "web", ArchiveURL: "https://example.com/web.tar.gz", ArchiveType: "tar.gz", State: StateDeployed},
					exists: true,
				},
				{
					name: "an app not deployed from an archive",
					task: GitFromArchiveTask{App: "other", ArchiveURL: "https://example.com/web.tar.gz", State: StateDeployed},
					want: GitFromArchiveTask{App: "other", State: StateAbsent},
				},
			},
		},
		"dokku_git_auth": {
			exitCodes: map[string]int{
				"--quiet git:auth-status github.com deploy-bot":  0,
				"--quiet git:auth-status gitlab.com deploy-bot":  2,
				"--quiet git:auth-status example.com deploy-bot": 1,
				"--quiet git:auth-status example.com":            0,
				"--quiet git:auth-status github.com":             2,
			},
			cases: []observedCase{
				{
					name:   "a matching entry",
					task:   GitAuthTask{Host: "github.com", Username: "deploy-bot", Password: "s3cr3t", State: StatePresent},
					want:   GitAuthTask{Host: "github.com", State: StatePresent},
					exists: true,
				},
				{
					name:   "a different entry",
					task:   GitAuthTask{Host: "gitlab.com", Username: "deploy-bot", Password: "s3cr3t", State: StatePresent},
					want:   GitAuthTask{Host: "gitlab.com", State: StatePresent},
					exists: true,
				},
				{
					name: "no entry, planned present",
					task: GitAuthTask{Host: "example.com", Username: "deploy-bot", Password: "s3cr3t", State: StatePresent},
					want: GitAuthTask{Host: "example.com", State: StateAbsent},
				},
				{
					name: "no entry, planned absent",
					task: GitAuthTask{Host: "example.com", State: StateAbsent},
					want: GitAuthTask{Host: "example.com", State: StateAbsent},
				},
				{
					name:   "an entry, planned absent",
					task:   GitAuthTask{Host: "github.com", State: StateAbsent},
					want:   GitAuthTask{Host: "github.com", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_app_clone": {
			responses: map[string]string{"--quiet apps:exists web": ""},
			exitCodes: map[string]int{"--quiet apps:exists gone": 1},
			cases: []observedCase{
				{
					name:   "the target exists",
					task:   AppCloneTask{App: "web", SourceApp: "template", State: StatePresent},
					want:   AppCloneTask{App: "web", State: StatePresent},
					exists: true,
				},
				{
					name: "the target is missing",
					task: AppCloneTask{App: "gone", SourceApp: "template", State: StatePresent},
					want: AppCloneTask{App: "gone", State: StateAbsent},
				},
			},
		},
		"dokku_app_lock": {
			responses: map[string]string{"--quiet apps:locked web": ""},
			exitCodes: map[string]int{"--quiet apps:locked open": 1},
			export:    exportAppFrom(AppLockTask{}),
			cases: []observedCase{
				{
					name: "an unlocked app",
					task: AppLockTask{App: "open", State: StatePresent},
					want: AppLockTask{App: "open", State: StateAbsent},
				},
				{
					name:   "a locked app, planned absent",
					task:   AppLockTask{App: "web", State: StateAbsent},
					want:   AppLockTask{App: "web", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_plugin": {
			responses: map[string]string{
				"--quiet plugin:list --format json": `[` +
					`{"name":"apps","core":true,"source_url":"","committish":"","branch":""},` +
					`{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"0a1b2c3","branch":"master"},` +
					`{"name":"letsencrypt","core":false,"source_url":"https://github.com/dokku/dokku-letsencrypt.git","committish":"9f8e7d6","branch":""}` +
					`]`,
			},
			export: exportGlobalFrom(PluginTask{}),
			cases: []observedCase{
				{
					name: "a plugin not installed",
					task: PluginTask{Name: "postgres", URL: "https://github.com/dokku/dokku-postgres.git", State: StatePresent},
					want: PluginTask{Name: "postgres", State: StateAbsent},
				},
				{
					name:   "a different source reads the installed one",
					task:   PluginTask{Name: "redis", URL: "https://example.com/fork.git", Committish: "v1", State: StatePresent},
					want:   PluginTask{Name: "redis", URL: "https://github.com/dokku/dokku-redis.git", Committish: "master", State: StatePresent},
					exists: true,
				},
				{
					name:   "a core plugin, planned absent",
					task:   PluginTask{Name: "apps", State: StateAbsent},
					want:   PluginTask{Name: "apps", State: StatePresent},
					exists: true,
				},
			},
		},
		"dokku_ssh_key": {
			responses: map[string]string{
				"--quiet ssh-keys:list --format json": `[{"name":"admin","fingerprint":"` + testSSHKey1Fingerprint + `","public-key":"` + testSSHKey1 + `"}]`,
			},
			export: exportGlobalFrom(SshKeyTask{}),
			cases: []observedCase{
				{
					name:   "a different key reads the installed one",
					task:   SshKeyTask{Name: "admin", Key: testSSHKey2, State: StatePresent},
					want:   SshKeyTask{Name: "admin", Key: testSSHKey1, State: StatePresent},
					exists: true,
				},
				{
					name: "a key not installed",
					task: SshKeyTask{Name: "deploy-bot", State: StateAbsent},
					want: SshKeyTask{Name: "deploy-bot", State: StateAbsent},
				},
			},
		},
		"dokku_acl_app": {
			responses: map[string]string{
				"--quiet acl:list web":   "alice\nbob\n",
				"--quiet acl:list empty": "",
			},
			export: exportAppFrom(AclAppTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the named users",
					task:   AclAppTask{App: "web", Users: []string{"alice", "carol"}, State: StatePresent},
					want:   AclAppTask{App: "web", Users: []string{"alice"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent with none of the named users",
					task: AclAppTask{App: "web", Users: []string{"carol"}, State: StateAbsent},
					want: AclAppTask{App: "web", State: StateAbsent},
				},
				{
					name: "clear on an empty list",
					task: AclAppTask{App: "empty", State: StateClear},
					want: AclAppTask{App: "empty", State: StateClear},
				},
			},
		},
		"dokku_acl_service": {
			responses: map[string]string{
				"--quiet plugin:trigger service-list":  "redis:cache\nredis:empty\n",
				"--quiet acl:list-service redis cache": "alice\nbob\n",
				"--quiet acl:list-service redis empty": "",
			},
			export: exportGlobalFrom(AclServiceTask{}),
			cases: []observedCase{
				{
					name:   "set reads every user",
					task:   AclServiceTask{Service: "cache", Type: "redis", Users: []string{"alice"}, State: StateSet},
					want:   AclServiceTask{Service: "cache", Type: "redis", Users: []string{"alice", "bob"}, State: StateSet},
					exists: true,
				},
				{
					name:   "absent reads the named users",
					task:   AclServiceTask{Service: "cache", Type: "redis", Users: []string{"bob", "carol"}, State: StateAbsent},
					want:   AclServiceTask{Service: "cache", Type: "redis", Users: []string{"bob"}, State: StatePresent},
					exists: true,
				},
				{
					name: "present on an empty list",
					task: AclServiceTask{Service: "empty", Type: "redis", Users: []string{"alice"}, State: StatePresent},
					want: AclServiceTask{Service: "empty", Type: "redis", State: StateAbsent},
				},
			},
		},
		"dokku_registry_auth": {
			responses: map[string]string{
				"--quiet registry:report web --format json": `{"auth-servers":"ghcr.io"}`,
			},
			exitCodes: map[string]int{
				"--quiet registry:auth-status --password-stdin web ghcr.io deploy-bot": 0,
				"--quiet registry:auth-status web docker.io":                           0,
				"--quiet registry:auth-status web quay.io":                             4,
				"--quiet registry:auth-status --password-stdin web quay.io deploy-bot": 1,
			},
			// The exported body names the server alone; export lifts the
			// write-only credential into inputs, which a recipe then fills.
			export: func(ctx context.Context) ([]interface{}, error) {
				bodies, err := RegistryAuthTask{}.ExportApp(ctx, "web")
				for i, body := range bodies {
					task := body.(RegistryAuthTask)
					task.Username = "deploy-bot"
					task.Password = "s3cr3t"
					bodies[i] = task
				}
				return bodies, err
			},
			cases: []observedCase{
				{
					name: "no credential, planned absent",
					task: RegistryAuthTask{App: "web", Server: "docker.io", State: StateAbsent},
					want: RegistryAuthTask{App: "web", Server: "docker.io", State: StateAbsent},
				},
				{
					name: "no credential, planned present",
					task: RegistryAuthTask{App: "web", Server: "quay.io", Username: "deploy-bot", Password: "s3cr3t", State: StatePresent},
					want: RegistryAuthTask{App: "web", Server: "quay.io", State: StateAbsent},
				},
				{
					name:   "an unreadable credential is still a credential",
					task:   RegistryAuthTask{App: "web", Server: "quay.io", State: StateAbsent},
					want:   RegistryAuthTask{App: "web", Server: "quay.io", State: StatePresent},
					exists: true,
				},
			},
		},
		// dokku_certs never reads certificate material at plan time, so the
		// exported body's PEM fields are tagged write_only and left out of the
		// comparison.
		"dokku_certs": {
			responses: mergeFixtures(certsAppFixture("web", observedCert), map[string]string{
				"--quiet certs:show web key":                                "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----",
				"--quiet certs:report gone --ssl-enabled":                   "false",
				"--quiet global-cert:report --global --global-cert-enabled": "false",
			}),
			export: exportAppFrom(CertsTask{}),
			cases: []observedCase{
				{
					name:   "an installed certificate, planned absent",
					task:   CertsTask{App: "web", State: StateAbsent},
					want:   CertsTask{App: "web", State: StatePresent},
					exists: true,
				},
				{
					name: "no certificate",
					task: CertsTask{App: "gone", State: StateAbsent},
					want: CertsTask{App: "gone", State: StateAbsent},
				},
				{
					name: "no global certificate",
					task: CertsTask{Global: true, State: StateAbsent},
					want: CertsTask{Global: true, State: StateAbsent},
				},
			},
		},
		"dokku_maintenance_custom_page": {
			responses: map[string]string{
				"maintenance:report web --format json":       `{"custom-page-sha256":"` + observedPageChecksum(page) + `"}`,
				"--quiet maintenance:custom-page-export web": observedPageTarball(page),
				"maintenance:report gone --format json":      `{"custom-page-sha256":""}`,
			},
			export: exportAppFrom(MaintenanceCustomPageTask{}),
			cases: []observedCase{
				{
					name:   "a different page reads the installed one",
					task:   MaintenanceCustomPageTask{App: "web", Content: "<html>other</html>", State: StatePresent},
					want:   MaintenanceCustomPageTask{App: "web", Content: page, State: StatePresent},
					exists: true,
				},
				{
					name: "no page",
					task: MaintenanceCustomPageTask{App: "gone", State: StateAbsent},
					want: MaintenanceCustomPageTask{App: "gone", State: StateAbsent},
				},
			},
		},
	}
}
