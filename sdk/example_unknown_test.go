package sdk_test

import (
	"fmt"

	"github.com/dokku/docket/sdk"
)

// A task checked before every value is known: the second user's hash comes
// from elsewhere, so it counts as set and only the first user's conflict,
// which its known fields decide, is reported.
func ExampleWithUnknown() {
	task, err := sdk.NewTask("dokku_http_auth_user")
	if err != nil {
		panic(err)
	}
	users := task.(*sdk.HttpAuthUserTask)
	users.Users = []sdk.HttpAuthUser{
		{Username: "a", Password: "p", Hash: "h"},
		{Username: "b"},
	}

	for _, v := range sdk.ConstraintViolations(users, sdk.WithUnknown("app", "users[1].hash")) {
		fmt.Printf("%s: %s\n", v.Field, v.Message)
	}
	// Output: users[0].password: 'users[0].password' and 'users[0].hash' are mutually exclusive
}
