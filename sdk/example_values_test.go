package sdk_test

import (
	"fmt"

	"github.com/dokku/docket/sdk"
)

// A task built from values a caller holds, such as a decoded JSON body, and
// read back with every field present: the ints come back as int64 and the
// unset state at its default.
func ExampleTaskFromValues() {
	task, err := sdk.TaskFromValues("dokku_ps_scale", map[string]any{
		"app":   "api",
		"scale": map[string]any{"web": 2, "worker": float64(1)},
	})
	if err != nil {
		panic(err)
	}
	values := sdk.TaskValues(task)
	fmt.Printf("%v %T %v %v\n", values["state"], values["scale"].(map[string]any)["web"], values["scale"], values["skip_deploy"])

	_, err = sdk.TaskFromValues("dokku_ps_scale", map[string]any{"scale": map[string]any{"web": "two"}})
	fmt.Println(err)
	// Output:
	// present int64 map[web:2 worker:1] false
	// dokku_ps_scale: scale["web"]: expected int, got string
}
