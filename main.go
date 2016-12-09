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

var (
	listen           string
	destination      string
	updateRate       int
	quotaDir         string
	usersFile        string
	sessionTimeout   int
	state            = make(State)
	quota            = make(Quota)
	scheduler        chan struct{}
	directoryWatcher chan struct{}
)

func scheduleCapacitiesUpdate() chan struct{} {
	ticker := time.NewTicker(time.Duration(updateRate) * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				refreshAllCapacities()
			case <-quit:
				{
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
				browserId.Name,
				browserId.Version,
			)
			refreshCapacities(maxConnections, *browserState)
		}
	}
}

func init() {
	flag.StringVar(&listen, "listen", ":8080", "Host and port to listen to")
	flag.StringVar(&destination, "destination", ":4444", "Host and port to proxy to")
	flag.IntVar(&updateRate, "updateRate", 5, "Time in seconds between refreshing queue lengths")
	flag.StringVar(&quotaDir, "quotaDir", "quota", "Directory to search for quota XML files")
	flag.StringVar(&usersFile, "users", "users.properties", "Path of the list of users")
	flag.IntVar(&sessionTimeout, "timeout", 300, "Session timeout in seconds")
	flag.Parse()
}

func waitForShutdown(shutdownAction func()) {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	shutdownAction()
}

func main() {
	directoryWatcher = LoadAndWatch(quotaDir, &quota)
	defer close(directoryWatcher)
	scheduler = scheduleCapacitiesUpdate()
	defer close(scheduler)
	go waitForShutdown(func() {
		log.Println("shutting down server")
		//TODO: wait for all connections to close with timeout
		os.Exit(0)
	})
	log.Println("listening on", listen)
	log.Println("destination host is", destination)
	http.ListenAndServe(listen, mux())
}
