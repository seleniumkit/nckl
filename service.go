package main

import (
	"bytes"
	"context"
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
)

const (
	wdHub             = "/wd/hub/"
	statusPath        = "/status"
	queuePath         = wdHub
	badRequestPath    = "/badRequest"
	badRequestMessage = "msg"
	slash             = "/"
	leaseKey          = "lease"
)

var (
	sessions       = make(Sessions)
	timeoutCancels = make(map[string]chan bool)
	leases         = make(map[string]Lease)
	sessionLock    sync.RWMutex
	stateLock      sync.Mutex
	updateLock     sync.Mutex
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

	ctx, _ := context.WithTimeout(r.Context(), requestTimeout)
	r = r.WithContext(ctx)

	err := requestInfo.error
	if err != nil {
		log.Printf("[INVALID_REQUEST] [%v]\n", err)
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
		process.AwaitQueue <- struct{}{}
		lease, disconnected := process.CapacityQueue.Push(r.Context())
		<-process.AwaitQueue
		if disconnected {
			log.Printf("[CLIENT_DISCONNECTED_FROM_QUEUE] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, processName, process.Priority)
			return
		}
		ctx = context.WithValue(ctx, leaseKey, lease)
		r = r.WithContext(ctx)
	}

}

type requestInfo struct {
	maxConnections int
	browser        BrowserId
	browserState   BrowserState
	processName    string
	process        *Process
	command        string
	lease          Lease
	error          error
}

func getRequestInfo(r *http.Request) *requestInfo {
	quotaName, _, _ := r.BasicAuth()
	stateLock.Lock()
	defer stateLock.Unlock()
	if _, ok := state[quotaName]; !ok {
		state[quotaName] = &QuotaState{}
	}
	quotaState := *state[quotaName]

	err, browserName, version, processName, priority, command := parsePath(r.URL)
	if err != nil {
		return &requestInfo{0, BrowserId{}, nil, "", nil, "", 0, err}
	}

	rawLease := r.Context().Value(leaseKey)
	var lease Lease
	if rawLease != nil {
		lease = rawLease.(Lease)
	} else {
		lease = Lease(0)
	}
	browserId := BrowserId{Name: browserName, Version: version}

	if _, ok := quotaState[browserId]; !ok {
		quotaState[browserId] = &BrowserState{}
	}
	browserState := *quotaState[browserId]

	maxConnections := quota.MaxConnections(quotaName, browserName, version)
	process := getProcess(browserState, processName, priority, maxConnections)

	return &requestInfo{maxConnections, browserId, browserState, processName, process, command, lease, nil}
}

type transport struct {
	http.RoundTripper
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == badRequestPath {
		return t.RoundTripper.RoundTrip(r)
	}

	requestInfo := getRequestInfo(r)
	command := requestInfo.command
	process := requestInfo.process
	isNewSessionRequest := isNewSessionRequest(r.Method, command)

	//Here we change request url
	r.URL.Scheme = "http"
	r.URL.Host = destination
	r.URL.Path = fmt.Sprintf("%s%s", wdHub, command)

	resp, err := t.RoundTripper.RoundTrip(r)

	browserId := requestInfo.browser
	select {
	case <-r.Context().Done():
		{
			log.Printf("[CLIENT_DISCONNECTED] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, requestInfo.processName, process.Priority)
			cleanupQueue(isNewSessionRequest, requestInfo)
		}
	default:
		{
			if err != nil {
				log.Printf("[REQUEST_ERROR] [%s %s] [%s] [%d] [%v]\n", browserId.Name, browserId.Version, requestInfo.processName, process.Priority, err)
				cleanupQueue(isNewSessionRequest, requestInfo)
			} else {
				processResponse(isNewSessionRequest, requestInfo, r, resp)
			}
		}
	}
	if r.Body != nil {
		r.Body.Close()
	}
	return resp, err
}

func processResponse(isNewSessionRequest bool, requestInfo *requestInfo, r *http.Request, resp *http.Response) {
	browserId := requestInfo.browser
	processName := requestInfo.processName
	process := requestInfo.process
	if isNewSessionRequest {
		if resp.StatusCode == http.StatusOK {
			body, _ := ioutil.ReadAll(resp.Body)
			var reply map[string]interface{}
			if json.Unmarshal(body, &reply) != nil {
				log.Printf("[JSON_ERROR] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, processName, process.Priority)
				cleanupQueue(isNewSessionRequest, requestInfo)
				return
			}
			rawSessionId := reply["sessionId"]
			switch rawSessionId.(type) {
			case string:
				{
					sessionId := rawSessionId.(string)

					cancelTimeout := make(chan bool)
					sessionLock.Lock()
					sessions[sessionId] = process
					timeoutCancels[sessionId] = cancelTimeout
					leases[sessionId] = requestInfo.lease
					sessionLock.Unlock()
					storage.AddSession(sessionId)
					go func() {
						select {
						case <-time.After(requestTimeout):
							{
								deleteSessionWithTimeout(sessionId, requestInfo, true)
							}
						case <-cancelTimeout:
						}
					}()
					storage.OnSessionDeleted(sessionId, func(id string) { deleteSession(id, requestInfo) })
					resp.Body.Close()
					resp.Body = ioutil.NopCloser(bytes.NewReader(body))
					log.Printf("[CREATED] [%s %s] [%s] [%d] [%s]\n", browserId.Name, browserId.Version, processName, process.Priority, sessionId)
					return
				}
			}
		}
		log.Printf("[NOT_CREATED] [%s %s] [%s] [%d]\n", browserId.Name, browserId.Version, processName, process.Priority)
		cleanupQueue(isNewSessionRequest, requestInfo)
	}

	if ok, sessionId := isDeleteSessionRequest(r.Method, requestInfo.command); ok {
		deleteSession(sessionId, requestInfo)
	}
}

func cleanupQueue(isNewSessionRequest bool, requestInfo *requestInfo) {
	if isNewSessionRequest {
		process := requestInfo.process
		process.CapacityQueue.Pop(requestInfo.lease)
	}
}

func deleteSession(sessionId string, requestInfo *requestInfo) {
	deleteSessionWithTimeout(sessionId, requestInfo, false)
}

func deleteSessionWithTimeout(sessionId string, requestInfo *requestInfo, timedOut bool) {
	browserId := requestInfo.browser
	processName := requestInfo.processName
	process := requestInfo.process

	sessionLock.RLock()
	process, ok := sessions[sessionId]
	cancel, _ := timeoutCancels[sessionId]
	sessionLock.RUnlock()
	if ok {
		if timedOut {
			log.Printf("[TIMED_OUT] [%s %s] [%s] [%d] [%s]\n", browserId.Name, browserId.Version, processName, process.Priority, sessionId)
		}
		log.Printf("[DELETING] [%s %s] [%s] [%d] [%s]\n", browserId.Name, browserId.Version, processName, process.Priority, sessionId)
		if cancel != nil {
			close(cancel)
		}
		sessionLock.Lock()
		delete(sessions, sessionId)
		delete(timeoutCancels, sessionId)
		lease := leases[sessionId]
		delete(leases, sessionId)
		sessionLock.Unlock()
		process.CapacityQueue.Pop(lease)
		log.Printf("[DELETED] [%s %s] [%s] [%d] [%s]\n", browserId.Name, browserId.Version, processName, process.Priority, sessionId)
	}
	storage.DeleteSession(sessionId)
}

func isNewSessionRequest(httpMethod string, command string) bool {
	return httpMethod == http.MethodPost && command == "session"
}

func isDeleteSessionRequest(httpMethod string, command string) (bool, string) {
	if httpMethod == http.MethodDelete && strings.HasPrefix(command, "session") {
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
	updateLock.Lock()
	defer updateLock.Unlock()
	if _, ok := browserState[name]; !ok {
		currentPriorities := getActiveProcessesPriorities(browserState)
		currentPriorities[name] = priority
		newCapacities := calculateCapacities(browserState, currentPriorities, maxConnections)
		browserState[name] = createProcess(priority, newCapacities[name])
		updateProcessCapacities(browserState, newCapacities)
	}
	process := browserState[name]
	process.Priority = priority
	process.LastActivity = time.Now()
	return process
}

func createProcess(priority int, capacity int) *Process {
	return &Process{
		Priority:      priority,
		AwaitQueue:    make(chan struct{}, math.MaxUint32),
		CapacityQueue: CreateQueue(capacity),
		LastActivity:  time.Now(),
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
	return len(process.AwaitQueue) > 0 || process.CapacityQueue.Size() > 0 || time.Now().Sub(process.LastActivity) < updateRate
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
	updateLock.Lock()
	defer updateLock.Unlock()
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
					Priority:     process.Priority,
					Queued:       len(process.AwaitQueue),
					Processing:   process.CapacityQueue.Size(),
					Max:          process.CapacityQueue.Capacity(),
					LastActivity: process.LastActivity.Format(time.UnixDate),
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
