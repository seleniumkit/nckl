package main

// This one should create independent channels for each user and priority and
// manage their buffer sizes dynamically updating them every N seconds. Sum
// of all queue channels should be always equal to total T specified in config.

type Process struct {
	priority      int
	awaitQueue    chan struct{}
	capacityQueue Queue
}

type Status struct {
	name       string `json:"name"`
	priority   int    `json:"priority"`
	queued     int    `json:"queued"`
	processing int    `json:"processing"`
}

type ProcessMetrics map[string]int

type QuotaState map[string]*Process

type State map[string]*QuotaState
