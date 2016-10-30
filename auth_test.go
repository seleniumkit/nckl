package main

import "testing"
import "github.com/aandryashin/matchers"

func TestPropertiesFileProvider(t *testing.T) {
	secrets := PropertiesFileProvider("test-data/test-users.properties")
	matchers.AssertThat(t, secrets("selenium", "anything"), matchers.EqualTo{"selenium-password"})
	matchers.AssertThat(t, secrets("test", "anything"), matchers.EqualTo{"test-password"})
	matchers.AssertThat(t, secrets("missing", "anything"), matchers.EqualTo{""})
}
