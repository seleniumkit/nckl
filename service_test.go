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
	"strings"
	"testing"
)

var (
	srv        *httptest.Server
	backendSrv *httptest.Server
)

const (
	username = "test"
	password = "test-password"
)

type MapStorage struct {
	m map[string][]func(string)
}

func NewMapStorage() *MapStorage {
	return &MapStorage{m: make(map[string][]func(string))}
}

func (storage *MapStorage) MembersCount() int {
	return 1
}

func (storage *MapStorage) AddSession(id string) {
	storage.m[id] = []func(string){}
}

func (storage *MapStorage) DeleteSession(id string) {
	for _, fn := range storage.m[id] {
		fn(id)
	}
	delete(storage.m, id)
}

func (storage *MapStorage) OnSessionDeleted(id string, fn func(string)) {
	storage.m[id] = append(storage.m[id], fn)
}

func (storage *MapStorage) Close() {
}

func init() {
	backendSrv = createBackendSrv(http.StatusOK)
	destination = hostFromServer(backendSrv)
	usersFile = "test-data/test-users.htpasswd"
	quotaDir = "test-data"
	srv = httptest.NewServer(mux())
	listen = hostFromServer(srv)
	storage = NewMapStorage()
	LoadAndWatch(quotaDir, &quota)
}

func hostFromServer(srv *httptest.Server) string {
	srvUrl, _ := url.Parse(srv.URL)
	return srvUrl.Host
}

func TestStatus(t *testing.T) {
	requestSession("first", 1)
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
	testUrl, _ := url.Parse("http://example.com/wd/hub/firefox/42.0/test-process/3/session/uuid/url")
	err, browserName, version, processName, priority, command := parsePath(testUrl)
	AssertThat(t, err, Is{nil})
	AssertThat(t, browserName, EqualTo{"firefox"})
	AssertThat(t, version, EqualTo{"42.0"})
	AssertThat(t, processName, EqualTo{"test-process"})
	AssertThat(t, priority, EqualTo{3})
	AssertThat(t, command, EqualTo{"session/uuid/url"})
}

func TestParsePathInvalidPriority(t *testing.T) {
	testUrl, _ := url.Parse("http://example.com/wd/hub/firefox/42.0/test-process/test/session")
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

func TestRound(t *testing.T) {
	AssertThat(t, round(1.51), EqualTo{2})
	AssertThat(t, round(1.49), EqualTo{1})
}

func TestCalculateCapacities(t *testing.T) {
	activeProcessesPriorities := make(ProcessMetrics)
	activeProcessesPriorities["first"] = 4
	activeProcessesPriorities["second"] = 1
	browserState := make(BrowserState)
	const maxConnections = 25
	newCapacities := calculateCapacities(browserState, activeProcessesPriorities, maxConnections)
	AssertThat(t, newCapacities["first"], EqualTo{20})
	AssertThat(t, newCapacities["second"], EqualTo{5})
}

func TestWaitInQueue(t *testing.T) {
	//We have 25 Firefox 33.0 according to test.xml.
	//20 of them should be used by first process and 5 by the second (proportion is 4:1).
	firstProcessTimeouts := 0
	secondProcessTimeouts := 0
	for i := 1; i <= 25; i++ {
		if requestTimeouts("first", 4) {
			firstProcessTimeouts++
		}
		if requestTimeouts("second", 1) {
			secondProcessTimeouts++
		}
	}
	AssertThat(t, firstProcessTimeouts, EqualTo{5 - 1}) //Because of capacity change there's one less request timeout
	AssertThat(t, secondProcessTimeouts, EqualTo{20})
}

func TestNotEnoughSessions(t *testing.T) {
	reqUrl := createUrl("/wd/hub/firefox/missing/test/1/session")
	resp, err := http.Post(
		reqUrl,
		"text/plain",
		strings.NewReader("payload"),
	)
	AssertThat(t, err, Is{nil})
	AssertThat(t, resp, Code{http.StatusBadRequest})
}

func TestInvalidRequest(t *testing.T) {
	reqUrl := createUrl("/wd/hub/session")
	resp, err := http.Post(
		reqUrl,
		"text/plain",
		strings.NewReader("payload"),
	)
	AssertThat(t, err, Is{nil})
	AssertThat(t, resp, Code{http.StatusBadRequest})
}

func TestDeleteSession(t *testing.T) {
	process := createProcess(1, 1)
	process.CapacityQueue.Push()
	sessions = make(Sessions)
	sessions["test-session"] = process
	AssertThat(t, process.CapacityQueue.Size(), EqualTo{1})
	reqUrl := createUrl("/wd/hub/firefox/33.0/test-process/1/session/test-session")
	req, _ := http.NewRequest(http.MethodDelete, reqUrl, strings.NewReader("payload"))
	resp, err := http.DefaultClient.Do(req)
	AssertThat(t, err, Is{nil})
	AssertThat(t, resp, Code{http.StatusOK})
	AssertThat(t, len(sessions), EqualTo{0})
	AssertThat(t, process.CapacityQueue.Size(), EqualTo{0})
}

func requestTimeouts(processName string, priority int) bool {
	return actionTimeouts(func() {
		requestSession(processName, priority)
	})
}

func requestSession(processName string, priority int) {
	reqUrl := createUrl(fmt.Sprintf("/wd/hub/firefox/33.0/%s/%d/session", processName, priority))
	http.Post(
		reqUrl,
		"text/plain",
		strings.NewReader("payload"),
	)
}

func createBackendSrv(statusCode int) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(`{"state":"success", "sessionId": "123", "value": {}}`))
	})
	return httptest.NewServer(mux)
}

func TestIsDeleteSessionRequest(t *testing.T) {
	state, sessionId := isDeleteSessionRequest("DELETE", "session/123")
	AssertThat(t, state, Is{true})
	AssertThat(t, sessionId, EqualTo{"123"})
	state, _ = isDeleteSessionRequest("DELETE", "session/123/cookie")
	AssertThat(t, state, Is{false})
}

func TestIsNewSessionRequest(t *testing.T) {
	AssertThat(t, isNewSessionRequest("POST", "session"), Is{true})
	AssertThat(t, isNewSessionRequest("POST", "session/123/timeouts"), Is{false})
}
