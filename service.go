package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abbot/go-http-auth"
	"math"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"bytes"
	"io/ioutil"
)

const (
	wdHub             = "/wd/hub/"
	statusPath        = "/status"
	queuePath         = wdHub
	badRequestPath    = "/badRequest"
	badRequestMessage = "msg"
	slash             = "/"
)

type requestsSet map[uint64]struct{}

var (
	sessions         = make(Sessions)
	sessionLock sync.Mutex
)

func badRequest(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get(badRequestMessage)
	if msg == "" {
		msg = "bad request"
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func queue(r *http.Request) {
	maxConnections, browserState, process, command, err := processAndMaxConnections(r)

	if err != nil {
		redirectToBadRequest(r, err.Error())
		return
	}

	if process.CapacityQueue.Capacity() == 0 {
		refreshCapacities(maxConnections, browserState)
		if process.CapacityQueue.Capacity() == 0 {
			redirectToBadRequest(r, "Not enough sessions for this process. Come back later.")
			return
		}
	}

	// Only new session requests should wait in queue
	if isNewSessionRequest(r.Method, command) {
		go func() {
			process.AwaitQueue <- struct{}{}
		}()
		process.CapacityQueue.Push()
		<-process.AwaitQueue
	}

	r.URL.Scheme = "http"
	r.URL.Host = destination
	r.URL.Path = fmt.Sprintf("%s%s", wdHub, command)
}

func processAndMaxConnections(r *http.Request) (int, BrowserState, *Process, string, error) {
	quotaName, _, _ := r.BasicAuth()
	if _, ok := state[quotaName]; !ok {
		state[quotaName] = &QuotaState{}
	}
	quotaState := *state[quotaName]

	err, browserName, version, processName, priority, command := parsePath(r.URL)
	if err != nil {
		return 0, nil, nil, "", err
	}

	browserId := BrowserId{Name: browserName, Version: version}

	if _, ok := quotaState[browserId]; !ok {
		quotaState[browserId] = &BrowserState{}
	}
	browserState := *quotaState[browserId]

	maxConnections := quota.MaxConnections(quotaName, browserName, version)
	process := getProcess(browserState, processName, priority, maxConnections)
	
	return maxConnections, browserState, process, command, nil
}

type transport struct {
	http.RoundTripper
}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	_, _, process, command, err := processAndMaxConnections(r)
	
	if isNewSessionRequest(r.Method, command) {
		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		var reply map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&reply)
		if (err != nil) {
			sessionId := reply["sessionId"].(string)
			sessionLock.Lock()
			sessions[sessionId] = process
			sessionLock.Unlock()
			go func() {
				timeout := time.Duration(sessionTimeout) * time.Second
				time.Sleep(timeout)
				deleteSession(sessionId)
			}()
			storage.OnSessionDeleted(sessionId, deleteSession)
		}
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
	}

	if ok, sessionId := isDeleteSessionRequest(r.Method, command); ok {
		deleteSession(sessionId)
	}
	
	return resp, nil
}

func deleteSession(sessionId string) {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if process, ok := sessions[sessionId]; ok {
		delete(sessions, sessionId)
		process.CapacityQueue.Pop()
	}
	storage.DeleteSession(sessionId)
}

func isNewSessionRequest(httpMethod string, command string) bool {
	return httpMethod == "POST" && command == "session"
}

func isDeleteSessionRequest(httpMethod string, command string) (bool, string) {
	
	if httpMethod == "DELETE" && strings.HasPrefix(command, "session") {
		pieces := strings.Split(command, "/")
		if (len(pieces) == 2) { //Against DELETE window url
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
		AwaitQueue:    make(chan struct{}, 2^64-1),
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

func mux() http.Handler {
	mux := http.NewServeMux()
	authenticator := auth.NewBasicAuthenticator(
		"Selenium Load Balancer",
		PropertiesFileProvider(usersFile),
	)
	proxyFunc := (&httputil.ReverseProxy{
		Director: queue,
		Transport: &transport{http.DefaultTransport},
	}).ServeHTTP
	mux.HandleFunc(queuePath, requireBasicAuth(authenticator, proxyFunc))
	mux.HandleFunc(statusPath, requireBasicAuth(authenticator, status))
	mux.HandleFunc(badRequestPath, badRequest)
	return mux
}
