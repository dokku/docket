package tasks

import (
	"testing"
)

func TestPluginTaskInvalidState(t *testing.T) {
	task := PluginTask{Name: "redis", URL: "https://github.com/dokku/dokku-redis.git", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPluginTaskRequiresName(t *testing.T) {
	result := PluginTask{URL: "https://github.com/dokku/dokku-redis.git", State: StatePresent}.Plan()
	if result.Error == nil {
		t.Fatal("Plan without 'name' should return an error")
	}
}

func TestPluginTaskRequiresURLWhenPresent(t *testing.T) {
	result := PluginTask{Name: "redis", State: StatePresent}.Plan()
	if result.Error == nil {
		t.Fatal("Plan with state 'present' and no 'url' should return an error")
	}
}
