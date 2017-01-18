package main

import (
	"flag"
	client "github.com/coreos/etcd/clientv3"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	listen           string
	destination      string
	updateRate       time.Duration
	quotaDir         string
	usersFile        string
	requestTimeout time.Duration
	endpoints        []string
	storage          Storage
	state            = make(State)
	quota            = make(Quota)
	scheduler        chan struct{}
	directoryWatcher chan struct{}
)

func scheduleCapacitiesUpdate() chan struct{} {
	ticker := time.NewTicker(updateRate)
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
	flag.DurationVar(&updateRate, "updateRate", 1 * time.Second, "Time between refreshing queue lengths like 1s or 500ms")
	flag.StringVar(&quotaDir, "quotaDir", "quota", "Directory to search for quota XML files")
	flag.StringVar(&usersFile, "users", "users.properties", "Path of the list of users")
	flag.DurationVar(&requestTimeout, "timeout", 300 * time.Second, "Session timeout like 3s or 500ms")
	var list string
	flag.StringVar(&list, "endpoints", "http://127.0.0.1:2379", "comma-separated list of etcd endpoints")
	flag.Parse()
	endpoints = strings.Split(list, ",")
}

func waitForShutdown(shutdownAction func()) {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	shutdownAction()
}

func createStorage() Storage {
	cfg := client.Config{
		Endpoints:   endpoints,
		DialTimeout: 10 * time.Second,
	}
	c, err := client.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Connected to storage with endpoints: %s\n", endpoints)
	return NewEtcdStorage(c)
}

func main() {
	directoryWatcher = LoadAndWatch(quotaDir, &quota)
	defer close(directoryWatcher)
	scheduler = scheduleCapacitiesUpdate()
	defer close(scheduler)
	storage = createStorage()
	defer storage.Close()
	go waitForShutdown(func() {
		log.Println("shutting down server")
		//TODO: wait for all connections to close with timeout
		os.Exit(0)
	})
	log.Println("listening on", listen)
	log.Println("destination host is", destination)
	server := &http.Server{Addr: listen, Handler: mux(), ReadTimeout: requestTimeout, WriteTimeout: requestTimeout}
	server.ListenAndServe()
}
