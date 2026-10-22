package sdk_test

import (
	"fmt"

	"github.com/dokku/docket/sdk"
)

// ExampleTaskRequires lists what a service link needs applied before it: the
// datastore plugin its service type names, then the app and the service it
// links, each created by its own task.
func ExampleTaskRequires() {
	task, err := sdk.NewTask("dokku_service_link")
	if err != nil {
		panic(err)
	}
	link := task.(*sdk.ServiceLinkTask)
	link.App = "api"
	link.Service = "redis"
	link.Name = "cache"

	for _, required := range sdk.TaskRequires(link) {
		fmt.Println(sdk.IdentityAddress(sdk.TaskTypeOf(required), required))
	}
	// Output:
	// dokku_plugin[name=redis]
	// dokku_app[app=api]
	// dokku_service_create[service=redis,name=cache]
}

// ExampleCatalogFor_requires reads the tasks an example needs applied first
// from the catalog, the same list the reference page prints with it.
func ExampleCatalogFor_requires() {
	catalog, err := sdk.CatalogFor([]string{"dokku_app_clone"})
	if err != nil {
		panic(err)
	}
	example := catalog.Tasks[0].Examples[0]
	fmt.Println(example.Name)
	for _, required := range example.Requires {
		fmt.Print(required.YAML)
	}
	// Output:
	// Clone an app
	// dokku_app:
	//     app: node-js-app
	// dokku_git_from_image:
	//     app: node-js-app
	//     image: dokku/smoke-test-app:dockerfile
}
