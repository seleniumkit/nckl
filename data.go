package main

import "fmt"

// This one should create independent channels for each user and priority and
// manage their buffer sizes dynamically updating them every N seconds. Sum
// of all queue channels should be always equal to total T specified in config.

type Process struct {
	Priority      int
	AwaitQueue    chan struct{}
	CapacityQueue Queue
	LastActivity  int64 //Unix timestamp in seconds
}

type BrowserStatus struct {
	Name      string                   `json:"name"`
	Processes map[string]ProcessStatus `json:"processes"`
}

type ProcessStatus struct {
	Priority   int `json:"priority"`
	Queued     int `json:"queued"`
	Processing int `json:"processing"`
	Max        int `json:"max"`
}

type ProcessMetrics map[string]int

type BrowserState map[string]*Process

type BrowserId struct {
	Name    string
	Version string
}

func (b BrowserId) String() string {
	return fmt.Sprintf("%s_%s", b.Name, b.Version)
}

type QuotaState map[BrowserId]*BrowserState

type State map[string]*QuotaState

type Sessions map[string]*Process
