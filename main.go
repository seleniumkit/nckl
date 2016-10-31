package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	statusPath        = "/status"
	queuePath         = "/session"
	badRequestPath    = "/badRequest"
	unknownUserPath   = "/unknownUser"
	badRequestMessage = "msg"
)

var (
	listen           = flag.String("listen", ":8080", "Host and port to listen to")
	destination      = flag.String("destination", ":4444", "Host and port to proxy to")
	updateRate       = flag.Int("updateRate", 5, "Time in seconds between refreshing queue lengths")
	quotaDir         = flag.String("quotaDirectory", "quota", "Directory to search for quota XML files")
	usersFile        = flag.String("usersFile", "users.properties", "Path of the list of users")
	state            = make(State)
	quota            = make(Quota)
	scheduler        chan struct{}
	directoryWatcher chan struct{}
)

func scheduleCapacitiesUpdate() chan struct{} {
	ticker := time.NewTicker(time.Duration(*updateRate) * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
				case <-ticker.C: refreshAllCapacities()
				case <-quit: {
					ticker.Stop()
					return
				}
			}
		}
	}()
	return quit
}

func refreshAllCapacities() {
	for quotaName, quotaState := range state {
		for browserId, browserState := range *quotaState {
			maxConnections := quota.MaxConnections(
				quotaName,
				browserId.name,
				browserId.version,
			)
			refreshCapacities(maxConnections, *browserState)
		}
	}
}

func init() {
	flag.Parse()
}

func waitForShutdown(shutdownAction func()) {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	shutdownAction()
}

func main() {
	directoryWatcher = LoadAndWatch(*quotaDir, &quota)
	defer close(directoryWatcher)
	scheduler = scheduleCapacitiesUpdate()
	defer close(scheduler)
	go waitForShutdown(func(){
		log.Println("shutting down server")
		//TODO: wait for all connections to close with timeout
		os.Exit(0)
	})
	log.Println("listening on", *listen)
	http.ListenAndServe(*listen, mux())
}
