package main

import "fmt"

// This one should create independent channels for each user and priority and
// manage their buffer sizes dynamically updating them every N seconds. Sum
// of all queue channels should be always equal to total T specified in config.

type Process struct {
	priority      int
	awaitQueue    chan struct{}
	capacityQueue Queue
}

type BrowserStatus struct {
	name      string `json:"name"`
	processes map[string]ProcessStatus `json:"processes"`
}

type ProcessStatus struct {
	priority   int    `json:"priority"`
	queued     int    `json:"queued"`
	processing int    `json:"processing"`
}

type ProcessMetrics map[string]int

type BrowserState map[string]*Process

type BrowserId struct {
	name string
	version string
}

func (b BrowserId) String() string {
	return fmt.Sprintf("%s:%s", b.name, b.version)
}

type QuotaState map[BrowserId]*BrowserState

type State map[string]*QuotaState
