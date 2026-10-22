package sdk_test

import (
	"context"

	"github.com/dokku/docket/sdk"
)

// A process that may be killed without a chance to call Close, such as a
// Terraform provider, can have its session close each SSH connection as soon
// as no command is using it, so none lingers after the last command.
func ExampleWithControlPersist() {
	session := sdk.NewSession(nil, sdk.WithControlPersist(0))

	ctx := session.Context(context.Background(), sdk.Target{Host: "deploy@dokku.example.com"})
	_ = ctx
}
