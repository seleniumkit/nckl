package main

import (
	"github.com/aandryashin/matchers"
	"testing"
)

func TestCorrectCaps(t *testing.T) {
	caps := correctCaps()
	matchers.AssertThat(t, caps.browser(), matchers.EqualTo{"firefox"})
	matchers.AssertThat(t, caps.version(), matchers.EqualTo{"33.0"})
	matchers.AssertThat(t, caps.processName(), matchers.EqualTo{"test-process"})
	matchers.AssertThat(t, caps.processPriority(), matchers.EqualTo{42})
}

func TestIncorrectCaps(t *testing.T) {
	caps := make(Caps)
	matchers.AssertThat(t, caps.browser(), matchers.EqualTo{""})
	matchers.AssertThat(t, caps.version(), matchers.EqualTo{""})
	matchers.AssertThat(t, caps.processName(), matchers.EqualTo{""})
	matchers.AssertThat(t, caps.processPriority(), matchers.EqualTo{1})
}

func correctCaps() Caps {
	caps := make(Caps)
	capsMap := make(map[string]interface{})
	capsMap["browserName"] = "firefox"
	capsMap["version"] = "33.0"
	capsMap["processName"] = "test-process"
	capsMap["processPriority"] = "42"
	caps["desiredCapabilities"] = capsMap
	return caps
}
