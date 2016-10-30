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

	processName := caps.processName()
	priority := caps.processPriority()
	process := getProcess(quotaState, processName, priority)
	go func() {
		process.awaitQueue <- struct{}{}
	}()
	if process.capacityQueue.Size() == 0 {
		refreshCapacities(quotaState)
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

func getProcess(quotaState QuotaState, name string, priority int) *Process {
	if _, ok := quotaState[name]; !ok {
		currentPriorities := getActiveProcessesPriorities(quotaState)
		currentPriorities[name] = priority
		newCapacities := calculateCapacities(quotaState, currentPriorities, maxConnections)
		quotaState[name] = createProcess(priority, newCapacities[name])
		updateProcessCapacities(quotaState, newCapacities)
	}
	process := quotaState[name]
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

func getActiveProcessesPriorities(quotaState QuotaState) ProcessMetrics {
	currentPriorities := make(ProcessMetrics)
	for name, process := range quotaState {
		if isProcessActive(process) {
			currentPriorities[name] = process.priority
		}
	}
	return currentPriorities
}

func isProcessActive(process *Process) bool {
	return len(process.awaitQueue) > 0 || process.capacityQueue.Size() > 0
}

func calculateCapacities(quotaState QuotaState, activeProcessesPriorities ProcessMetrics, maxConnections int) ProcessMetrics {
	sumOfPriorities := 0
	for _, priority := range activeProcessesPriorities {
		sumOfPriorities += priority
	}
	ret := ProcessMetrics{}
	for processName := range quotaState {
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

func updateProcessCapacities(quotaState QuotaState, newCapacities ProcessMetrics) {
	for processName, newCapacity := range newCapacities {
		process := quotaState[processName]
		process.capacityQueue.SetCapacity(newCapacity)
	}
}

func refreshCapacities(quotaState QuotaState) {
	currentPriorities := getActiveProcessesPriorities(quotaState)
	newCapacities := calculateCapacities(quotaState, currentPriorities, maxConnections)
	updateProcessCapacities(quotaState, newCapacities)
}

func status(w http.ResponseWriter, r *http.Request) {
	quotaName, _, _ := r.BasicAuth()
	status := []Status{}
	if _, ok := state[quotaName]; ok {
		quotaState := state[quotaName]
		for processName, process := range *quotaState {
			status = append(status, Status{
				name:       processName,
				priority:   process.priority,
				queued:     len(process.awaitQueue),
				processing: process.capacityQueue.Size(),
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
