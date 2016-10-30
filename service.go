package main

import (
	"encoding/json"
	"github.com/abbot/go-http-auth"
	"math"
	"net/http"
	"net/http/httputil"
)

func badRequest(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get(badRequestMessage)
	if msg == "" {
		msg = "bad request"
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func unknownUser(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Unknown user", http.StatusUnauthorized)
}

func queue(r *http.Request) {
	quotaName, _, _ := r.BasicAuth()
	if _, ok := state[quotaName]; !ok {
		state[quotaName] = &QuotaState{}
	}
	quotaState := *state[quotaName]

	caps, err := getCaps(r)
	if err != nil {
		r.URL.Path = badRequestPath
		r.URL.Query().Add(badRequestMessage, "Failed to parse capabilities JSON")
		return
	}

	browserName := caps.browser()
	version := caps.version()
	browserId := BrowserId{name: browserName, version:version}
	
	if _, ok := quotaState[browserId]; !ok {
		quotaState[browserId] = &BrowserState{}
	}
	browserState := *quotaState[browserId]
	
	processName := caps.processName()
	priority := caps.processPriority()
	maxConnections := quota.MaxConnections(quotaName, browserName, version)
	process := getProcess(browserState, processName, priority, maxConnections)
	go func() {
		process.awaitQueue <- struct{}{}
	}()
	if process.capacityQueue.Size() == 0 {
		refreshCapacities(maxConnections, browserState)
		if process.capacityQueue.Size() == 0 {
			r.URL.Path = badRequestPath
			r.URL.Query().Add(badRequestMessage, "Not enough sessions for this process. Come back later.")
			return
		}
	}
	process.capacityQueue.Push()
	<-process.awaitQueue
	r.URL.Host = *destination
}

func getCaps(r *http.Request) (Caps, error) {
	var c Caps
	e := json.NewDecoder(r.Body).Decode(&c)
	return c, e
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
	process.priority = priority
	return process
}

func createProcess(priority int, capacity int) *Process {
	return &Process{
		priority:      priority,
		awaitQueue:    make(chan struct{}, 2^64-1),
		capacityQueue: CreateQueue(capacity),
	}
}

func getActiveProcessesPriorities(browserState BrowserState) ProcessMetrics {
	currentPriorities := make(ProcessMetrics)
	for name, process := range browserState {
		if isProcessActive(process) {
			currentPriorities[name] = process.priority
		}
	}
	return currentPriorities
}

func isProcessActive(process *Process) bool {
	return len(process.awaitQueue) > 0 || process.capacityQueue.Size() > 0
}

func calculateCapacities(browserState BrowserState, activeProcessesPriorities ProcessMetrics, maxConnections int) ProcessMetrics {
	sumOfPriorities := 0
	for _, priority := range activeProcessesPriorities {
		sumOfPriorities += priority
	}
	ret := ProcessMetrics{}
	for processName := range browserState {
		if priority, ok := activeProcessesPriorities[processName]; ok {
			ret[processName] = round(float64(priority / sumOfPriorities * maxConnections))
		} else {
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
		process.capacityQueue.SetCapacity(newCapacity)
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
					priority:   process.priority,
					queued:     len(process.awaitQueue),
					processing: process.capacityQueue.Size(),
				}
			}
			status = append(status, BrowserStatus{
				name: browserId.String(),
				processes: processes,
			})
		}
	}
	json.NewEncoder(w).Encode(&status)
}

func requireBasicAuth(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	authenticator := auth.NewBasicAuthenticator(
		"Selenium Load Balancer",
		PropertiesFileProvider(*usersFile),
	)
	return authenticator.Wrap(func(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
		handler(w, &r.Request)
	})
}

func mux() http.Handler {
	mux := http.NewServeMux()
	proxyFunc := (&httputil.ReverseProxy{Director: queue}).ServeHTTP
	mux.HandleFunc(queuePath, requireBasicAuth(proxyFunc))
	mux.HandleFunc(statusPath, requireBasicAuth(status))
	mux.HandleFunc(badRequestPath, badRequest)
	mux.HandleFunc(unknownUserPath, unknownUser)
	return mux
}
