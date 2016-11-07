package main

import (
	"fmt"
	. "github.com/aandryashin/matchers"
	. "github.com/aandryashin/matchers/httpresp"
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
