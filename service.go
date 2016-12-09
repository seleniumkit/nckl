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
	num         uint64
	requests    requestsSet = make(requestsSet)
	numLock     sync.Mutex
	requestLock sync.Mutex
)

func badRequest(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get(badRequestMessage)
	if msg == "" {
		msg = "bad request"
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func queue(r *http.Request) {
	quotaName, _, _ := r.BasicAuth()
	if _, ok := state[quotaName]; !ok {
		state[quotaName] = &QuotaState{}
	}
	quotaState := *state[quotaName]

	err, browserName, version, processName, priority, command := parsePath(r.URL)
	if err != nil {
		redirectToBadRequest(r, err.Error())
		return
	}

	browserId := BrowserId{Name: browserName, Version: version}

	if _, ok := quotaState[browserId]; !ok {
		quotaState[browserId] = &BrowserState{}
	}
	browserState := *quotaState[browserId]

	maxConnections := quota.MaxConnections(quotaName, browserName, version)
	requestLock.Lock()
	process := getProcess(browserState, processName, priority, maxConnections)
	requestLock.Unlock()

	if process.CapacityQueue.Capacity() == 0 {
		refreshCapacities(maxConnections, browserState)
		if process.CapacityQueue.Capacity() == 0 {
			redirectToBadRequest(r, "Not enough sessions for this process. Come back later.")
			return
		}
	}

	// Only new session requests should wait in queue
	requestId := serial()
	if isNewSessionRequest(r.Method, command) {
		go func() {
			process.AwaitQueue <- struct{}{}
		}()
		requestLock.Lock()
		requests[requestId] = struct{}{}
		requestLock.Unlock()
		process.CapacityQueue.Push()
		<-process.AwaitQueue
		go func() {
			timeout := time.Duration(sessionTimeout) * time.Second
			time.Sleep(timeout)
			finalizeSession(requests, requestId, process)
		}()
	}

	if isDeleteSessionRequest(r.Method, command) {
		finalizeSession(requests, requestId, process)
	}

	r.URL.Scheme = "http"
	r.URL.Host = destination
	r.URL.Path = fmt.Sprintf("%s%s", wdHub, command)
}

func serial() uint64 {
	numLock.Lock()
	defer numLock.Unlock()
	id := num
	num++
	return id
}

func finalizeSession(requests requestsSet, requestId uint64, process *Process) {
	requestLock.Lock()
	defer requestLock.Unlock()
	if _, ok := requests[requestId]; ok {
		delete(requests, requestId)
		process.CapacityQueue.Pop()
	}
}

func isNewSessionRequest(httpMethod string, command string) bool {
	return httpMethod == "POST" && command == "session"
}

func isDeleteSessionRequest(httpMethod string, command string) bool {
	return httpMethod == "DELETE" &&
		strings.HasPrefix(command, "session") &&
		len(strings.Split(command, "/")) == 2 //Against DELETE window url
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
		ret[processName] = round(float64(priority) / float64(sumOfPriorities) * float64(maxConnections))
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
	proxyFunc := (&httputil.ReverseProxy{Director: queue}).ServeHTTP
	mux.HandleFunc(queuePath, requireBasicAuth(authenticator, proxyFunc))
	mux.HandleFunc(statusPath, requireBasicAuth(authenticator, status))
	mux.HandleFunc(badRequestPath, badRequest)
	return mux
}
