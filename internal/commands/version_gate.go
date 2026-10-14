package commands

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/internal/tasks"
)

// ignoreVersionHint adds the flag that skips a version refusal to err. The
// engine's *tasks.VersionError names no flag, because an sdk caller has none
// to pass; any other error is returned unchanged.
func ignoreVersionHint(err error) error {
	var verr *tasks.VersionError
	if errors.As(err, &verr) {
		return fmt.Errorf("%w (pass --ignore-version to run it anyway)", err)
	}
	return err
}
