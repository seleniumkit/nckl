package main

import (
	"bytes"
	"fmt"
	. "github.com/aandryashin/matchers"
	. "github.com/aandryashin/matchers/httpresp"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

var (
	srv *httptest.Server
)

const (
	username = "selenium"
	password = "selenium-password"
)

func init() {
	srv = httptest.NewServer(mux("test-data/test-users.properties"))
}

func TestStatus(t *testing.T) {
	rsp, err := http.Post(createUrl("/status"), "", nil)
	AssertThat(t, err, Is{nil})
	AssertThat(t, rsp, Code{http.StatusOK})
}

func createUrl(path string) string {
	parsedUrl, _ := url.Parse(fmt.Sprintf("%s%s", srv.URL, path))
	parsedUrl.User = url.UserPassword(username, password)
	return parsedUrl.String()
}

func TestParseCorrectPath(t *testing.T) {
	testUrl, _ := url.Parse("http://example.com/firefox/42.0/test-process/3/session")
	err, browserName, version, processName, priority, command := parsePath(testUrl)
	AssertThat(t, err, Is{nil})
	AssertThat(t, browserName, EqualTo{"firefox"})
	AssertThat(t, version, EqualTo{"42.0"})
	AssertThat(t, processName, EqualTo{"test-process"})
	AssertThat(t, priority, EqualTo{3})
	AssertThat(t, command, EqualTo{"session"})
}

func TestParsePathInvalidPriority(t *testing.T) {
	testUrl, _ := url.Parse("http://example.com/firefox/42.0/test-process/test/session")
	err, _, _, _, priority, _ := parsePath(testUrl)
	AssertThat(t, err, Is{nil})
	AssertThat(t, priority, EqualTo{1})
}

func TestParseInvalidPath(t *testing.T) {
	testUrl, _ := url.Parse("http://example.com/invalid")
	err, _, _, _, _, _ := parsePath(testUrl)
	AssertThat(t, err, Is{Not{nil}})
}

func TestBadRequest(t *testing.T) {
	badRequestUrl := fmt.Sprintf("%s?%s=%s", createUrl(badRequestPath), badRequestMessage, "test")
	resp, err := http.Get(badRequestUrl)
	AssertThat(t, err, Is{nil})
	AssertThat(t, resp.StatusCode, EqualTo{http.StatusBadRequest})
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	AssertThat(t, bytes.ContainsAny(bodyBytes, "test"), Is{true})
}
