package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abbot/go-http-auth"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"context"
)

const (
	wdHub             = "/wd/hub/"
	statusPath        = "/status"
	queuePath         = wdHub
	badRequestPath    = "/badRequest"
	badRequestMessage = "msg"
	slash             = "/"
)

var (
	sessions    = make(Sessions)
	sessionLock sync.RWMutex
)

func badRequest(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get(badRequestMessage)
	if msg == "" {
		msg = "bad request"
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func queue(r *http.Request) {
	requestInfo := getRequestInfo(r)

	ctx, _ := context.WithTimeout(r.Context(), sessionTimeout)
	r = r.WithContext(ctx)

	err := requestInfo.error
	if err != nil {
		log.Printf("[UNEXPECTED_ERROR] [%v]\n", err)
		redirectToBadRequest(r, err.Error())
		return
	}

	// Only new session requests should wait in queue
	command := requestInfo.command
	browserId := requestInfo.browser
	processName := requestInfo.processName
	process := requestInfo.process
	if isNewSessionRequest(r.Method, command) {
		log.Printf("[CREATING] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, processName, process.Priority)
		if process.CapacityQueue.Capacity() == 0 {
			refreshCapacities(requestInfo.maxConnections, requestInfo.browserState)
			if process.CapacityQueue.Capacity() == 0 {
				log.Printf("[NOT_ENOUGH_SESSIONS] [%s %s] [%s]\n", browserId.Name, browserId.Version, processName)
				redirectToBadRequest(r, "Not enough sessions for this process. Come back later.")
				return
			}
		}
		go func() {
			process.AwaitQueue <- struct{}{}
		}()
		process.CapacityQueue.Push()
		<-process.AwaitQueue
	}

}

type requestInfo struct {
	maxConnections int
	browser        BrowserId
	browserState   BrowserState
	processName    string
	process        *Process
	command        string
	error          error
}

func getRequestInfo(r *http.Request) *requestInfo {
	quotaName, _, _ := r.BasicAuth()
	if _, ok := state[quotaName]; !ok {
		state[quotaName] = &QuotaState{}
	}
	quotaState := *state[quotaName]

	err, browserName, version, processName, priority, command := parsePath(r.URL)
	if err != nil {
		return &requestInfo{0, BrowserId{}, nil, "", nil, "", err}
	}

	browserId := BrowserId{Name: browserName, Version: version}

	if _, ok := quotaState[browserId]; !ok {
		quotaState[browserId] = &BrowserState{}
	}
	browserState := *quotaState[browserId]

	maxConnections := quota.MaxConnections(quotaName, browserName, version)
	process := getProcess(browserState, processName, priority, maxConnections)

	return &requestInfo{maxConnections, browserId, browserState, processName, process, command, nil}
}

type transport struct {
	http.RoundTripper
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	requestInfo := getRequestInfo(r)
	command := requestInfo.command
	browserId := requestInfo.browser
	isNewSessionRequest := isNewSessionRequest(r.Method, command)

	if requestInfo.error != nil {
		cleanupQueue(isNewSessionRequest, requestInfo)
		return nil, errors.New(fmt.Sprintf("[FAILED] [%v]\n", requestInfo.error))
	}

	//Here we change request url
	r.URL.Scheme = "http"
	r.URL.Host = destination
	r.URL.Path = fmt.Sprintf("%s%s", wdHub, command)

	resp, err := t.RoundTripper.RoundTrip(r)
	select {
	case <-r.Context().Done():
		log.Printf("[CLIENT_DISCONNECTED]  [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, requestInfo.processName, requestInfo.process.Priority)
		cleanupQueue(isNewSessionRequest, requestInfo)
		return nil, errors.New("Client disconnected")
	default:
	}
	if err != nil {
		cleanupQueue(isNewSessionRequest, requestInfo)
		return nil, err
	}
	if (resp != nil) {
		defer resp.Body.Close()
	}

	if isNewSessionRequest {
		body, _ := ioutil.ReadAll(resp.Body)
		var reply map[string]interface{}
		err = json.Unmarshal(body, &reply)
		if err != nil {
			cleanupQueue(isNewSessionRequest, requestInfo)
			return nil, err
		}
		sessionId := reply["sessionId"].(string)
		sessionLock.Lock()
		sessions[sessionId] = requestInfo.process
		sessionLock.Unlock()
		storage.AddSession(sessionId)
		go func() {
			time.Sleep(sessionTimeout)
			deleteSessionWithTimeout(sessionId, true)
		}()
		storage.OnSessionDeleted(sessionId, deleteSession)
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
		log.Printf("[CREATED] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, requestInfo.processName, requestInfo.process.Priority)
	}

	if ok, sessionId := isDeleteSessionRequest(r.Method, command); ok {
		deleteSession(sessionId)
	}

	return resp, nil
}

func cleanupQueue(isNewSessionRequest bool, requestInfo *requestInfo) {
	if isNewSessionRequest {
		requestInfo.process.CapacityQueue.Pop()
	}
}

func deleteSession(sessionId string) {
	deleteSessionWithTimeout(sessionId, false)
}

func deleteSessionWithTimeout(sessionId string, timedOut bool) {
	sessionLock.RLock()
	process, ok := sessions[sessionId]
	sessionLock.RUnlock()
	if ok {
		if timedOut {
			log.Printf("[TIMED_OUT] [%s]\n", sessionId)
		}
		log.Printf("[DELETING] [%s]\n", sessionId)
		sessionLock.Lock()
		delete(sessions, sessionId)
		sessionLock.Unlock()
		process.CapacityQueue.Pop()
		log.Printf("[DELETED] [%s]\n", sessionId)
	}
	storage.DeleteSession(sessionId)
}

func isNewSessionRequest(httpMethod string, command string) bool {
	return httpMethod == "POST" && command == "session"
}

func isDeleteSessionRequest(httpMethod string, command string) (bool, string) {

	if httpMethod == "DELETE" && strings.HasPrefix(command, "session") {
		pieces := strings.Split(command, slash)
		if len(pieces) == 2 { //Against DELETE window url
			return true, pieces[1]
		}
	}
	return false, ""
}

func redirectToBadRequest(r *http.Request, msg string) {
	r.URL.Scheme = "http"
	r.URL.Host = listen
	r.Method = "GET"
	r.URL.Path = badRequestPath
	values := r.URL.Query()
	values.Set(badRequestMessage, msg)
	r.URL.RawQuery = values.Encode()
}

func parsePath(url *url.URL) (error, string, string, string, int, string) {
	p := strings.Split(strings.TrimPrefix(url.Path, wdHub), slash)
	if len(p) < 5 {
		err := errors.New(fmt.Sprintf("invalid url [%s]: should have format /browserName/version/processName/priority/command", url))
		return err, "", "", "", 0, ""
	}
	priority, err := strconv.Atoi(p[3])
	if err != nil {
		priority = 1
	}
	return nil, p[0], p[1], p[2], priority, strings.Join(p[4:], slash)
}

func getProcess(browserState BrowserState, name string, priority int, maxConnections int) *Process {
	if _, ok := browserState[name]; !ok {
		currentPriorities := getActiveProcessesPriorities(browserState)
		currentPriorities[name] = priority
		newCapacities := calculateCapacities(browserState, currentPriorities, maxConnections)
		browserState[name] = createProcess(priority, newCapacities[name])
		updateProcessCapacities(browserState, newCapacities)
	}
	process := browserState[name]
	process.Priority = priority
	process.LastActivity = time.Now().Unix()
	return process
}

func createProcess(priority int, capacity int) *Process {
	return &Process{
		Priority:      priority,
		AwaitQueue:    make(chan struct{}, math.MaxUint32),
		CapacityQueue: CreateQueue(capacity),
		LastActivity:  time.Now().Unix(),
	}
}

func getActiveProcessesPriorities(browserState BrowserState) ProcessMetrics {
	currentPriorities := make(ProcessMetrics)
	for name, process := range browserState {
		if isProcessActive(process) {
			currentPriorities[name] = process.Priority
		}
	}
	return currentPriorities
}

func isProcessActive(process *Process) bool {
	lastActivitySeconds := time.Now().Unix() - process.LastActivity
	return len(process.AwaitQueue) > 0 || process.CapacityQueue.Size() > 0 || lastActivitySeconds < int64(updateRate)
}

func calculateCapacities(browserState BrowserState, activeProcessesPriorities ProcessMetrics, maxConnections int) ProcessMetrics {
	sumOfPriorities := 0
	for _, priority := range activeProcessesPriorities {
		sumOfPriorities += priority
	}
	ret := ProcessMetrics{}
	for processName, priority := range activeProcessesPriorities {
		ret[processName] = round(float64(priority) / float64(sumOfPriorities) * float64(maxConnections) / float64(storage.MembersCount()))
	}
	for processName := range browserState {
		if _, ok := activeProcessesPriorities[processName]; !ok {
			ret[processName] = 0
		}
	}
	return ret
}

func round(num float64) int {
	i, frac := math.Modf(num)
	if frac < 0.5 {
		return int(i)
	} else {
		return int(i + 1)
	}
}

func updateProcessCapacities(browserState BrowserState, newCapacities ProcessMetrics) {
	for processName, newCapacity := range newCapacities {
		process := browserState[processName]
		process.CapacityQueue.SetCapacity(newCapacity)
	}
}

func refreshCapacities(maxConnections int, browserState BrowserState) {
	currentPriorities := getActiveProcessesPriorities(browserState)
	newCapacities := calculateCapacities(browserState, currentPriorities, maxConnections)
	updateProcessCapacities(browserState, newCapacities)
}

func status(w http.ResponseWriter, r *http.Request) {
	quotaName, _, _ := r.BasicAuth()
	status := []BrowserStatus{}
	if _, ok := state[quotaName]; ok {
		quotaState := state[quotaName]
		for browserId, browserState := range *quotaState {
			processes := make(map[string]ProcessStatus)
			for processName, process := range *browserState {
				processes[processName] = ProcessStatus{
					Priority:   process.Priority,
					Queued:     len(process.AwaitQueue),
					Processing: process.CapacityQueue.Size(),
					Max:        process.CapacityQueue.Capacity(),
				}
			}
			status = append(status, BrowserStatus{
				Name:      browserId.String(),
				Processes: processes,
			})
		}
	}
	json.NewEncoder(w).Encode(&status)
}

func requireBasicAuth(authenticator *auth.BasicAuth, handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return authenticator.Wrap(func(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
		handler(w, &r.Request)
	})
}

func withCloseNotifier(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		go func() {
			handler(w, r.WithContext(ctx))
			cancel()
		}()
		select {
		case <-w.(http.CloseNotifier).CloseNotify():
			cancel()
		case <-ctx.Done():
		}
	}
}

func mux() http.Handler {
	mux := http.NewServeMux()
	authenticator := auth.NewBasicAuthenticator(
		"Selenium Load Balancer",
		auth.HtpasswdFileProvider(usersFile),
	)
	proxyFunc := (&httputil.ReverseProxy{
		Director:  queue,
		Transport: &transport{http.DefaultTransport},
	}).ServeHTTP
	mux.HandleFunc(queuePath, requireBasicAuth(authenticator, withCloseNotifier(proxyFunc)))
	mux.HandleFunc(statusPath, requireBasicAuth(authenticator, status))
	mux.HandleFunc(badRequestPath, badRequest)
	return mux
}
