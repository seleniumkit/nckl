package main

import (
	"testing"
	"github.com/aandryashin/matchers"
)

func TestLoadAndWatch(t *testing.T) {
	quota := make(Quota)
	fileWatcher := LoadAndWatch("test-data", &quota)
	defer close(fileWatcher)
	matchers.AssertThat(t, quota.MaxConnections("test", "firefox", "33.0"), matchers.EqualTo{25})
	matchers.AssertThat(t, quota.MaxConnections("test", "chrome", "42.0"), matchers.EqualTo{40})
	matchers.AssertThat(t, quota.MaxConnections("test", "missing", "any"), matchers.EqualTo{0})
	matchers.AssertThat(t, quota.MaxConnections("missing", "any", "any"), matchers.EqualTo{0})
}
