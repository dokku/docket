package sdk_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// TestReadEmptyCollectionExists pins #639 through the sdk: an app with no
// buildpacks has an empty buildpack list, which exists, whether it is read
// from its address or from a task that owns the whole list. A task in an
// additive state still reports the member it names as missing.
func TestReadEmptyCollectionExists(t *testing.T) {
	ctx := dokkutest.New(t).SeedApp("web").Context(context.Background())

	got, exists, err := sdk.ReadAddress(ctx, "dokku_buildpacks[app=web]")
	if err != nil {
		t.Fatal(err)
	}
	if want := (sdk.BuildpacksTask{App: "web", State: sdk.StateClear}); !exists || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadAddress = %#v (exists %v), want %#v existing", got, exists, want)
	}

	set := sdk.BuildpacksTask{App: "web", Buildpacks: []string{"https://github.com/heroku/heroku-buildpack-ruby.git"}, State: sdk.StateSet}
	if _, exists, err := sdk.ReadTask(ctx, set); err != nil || !exists {
		t.Errorf("ReadTask set: exists %v, err %v; want an empty list that exists", exists, err)
	}

	present := set
	present.State = sdk.StatePresent
	if _, exists, err := sdk.ReadTask(ctx, present); err != nil || exists {
		t.Errorf("ReadTask present: exists %v, err %v; want the named buildpack missing", exists, err)
	}
}
