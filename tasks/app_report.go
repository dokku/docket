package tasks

import (
	"context"
	"encoding/json"
	"github.com/dokku/docket/subprocess"
)

// AppDeploySource contains deploy source information from apps:report
type AppDeploySource struct {
	Source         string `json:"app-deploy-source"`
	SourceMetadata string `json:"app-deploy-source-metadata"`
}

// getAppDeploySource retrieves the deploy source for an app via apps:report
func getAppDeploySource(ctx context.Context, app string) (AppDeploySource, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"apps:report", app, "--format", "json"},
	})
	if err != nil {
		return AppDeploySource{}, err
	}

	var source AppDeploySource
	if err := json.Unmarshal(result.StdoutBytes(), &source); err != nil {
		return AppDeploySource{}, err
	}

	return source, nil
}
