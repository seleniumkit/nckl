package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"sync"
)

type Browsers struct {
	XMLName  xml.Name  `xml:"urn:config.gridrouter.qatools.ru browsers"`
	Browsers []Browser `xml:"browser"`
}

type Browser struct {
	Name           string    `xml:"name,attr"`
	DefaultVersion string    `xml:"defaultVersion,attr"`
	Versions       []Version `xml:"version"`
}

type Version struct {
	Number  string   `xml:"number,attr"`
	Regions []Region `xml:"region"`
}

type Hosts []Host

type Region struct {
	Name  string `xml:"name,attr"`
	Hosts Hosts  `xml:"host"`
}

type Host struct {
	Name  string `xml:"name,attr"`
	Port  int    `xml:"port,attr"`
	Count int    `xml:"count,attr"`
}

type Quota map[string]int

var (
	loadLock sync.Mutex
)

func LoadAndWatch(quotaDir string, quota *Quota) chan struct{} {
	load(quotaDir, quota)
	return watchDir(quotaDir, quota)
}

func watchDir(quotaDir string, quota *Quota) chan struct{} {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create directory watcher: %v\n", err)
	}
	cancel := make(chan struct{})
	go func() {
	loop:
		for {
			select {
			case event := <-watcher.Events:
				{
					if event.Op&fsnotify.Write == fsnotify.Write {
						file := event.Name
						log.Printf("file %s changed:\n", file)
						loadFile(file, quota)
					}
				}
			case err := <-watcher.Errors:
				log.Printf("file watching error: %v\n", err)
			case <-cancel:
				break loop
			}
		}
		watcher.Close()
	}()
	err = watcher.Add(quotaDir)
	if err != nil {
		log.Printf("failed to start watching directory %s: %v", quotaDir, err)
	}
	return cancel
}

func load(quotaDir string, quota *Quota) {
	glob := fmt.Sprintf("%s%c%s", quotaDir, filepath.Separator, "*.xml")
	files, err := filepath.Glob(glob)
	if err != nil {
		log.Printf("failed to read quota XML files from [%s]: %v\n", quotaDir, err)
		return
	}
	for _, file := range files {
		loadFile(file, quota)
	}
}

func loadFile(file string, quota *Quota) {
	loadLock.Lock()
	log.Printf("loading quota information for file: %s\n", file)
	browsers, err := fileToBrowsers(file)
	if err != nil {
		log.Printf("%v", err)
	}
	fileName := filepath.Base(file)
	// Just file name without extension
	quotaName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	browsersToQuota(*quota, quotaName, *browsers)
	loadLock.Unlock()
}

func fileToBrowsers(file string) (*Browsers, error) {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("error reading configuration file %s: %v\n", file, err))
	}
	browsers := new(Browsers)
	if err := xml.Unmarshal(bytes, browsers); err != nil {
		return nil, errors.New(fmt.Sprintf("error parsing configuration file %s: %v\n", file, err))
	}
	return browsers, nil
}

func browsersToQuota(quota Quota, quotaName string, browsers Browsers) {
	for _, b := range browsers.Browsers {
		browserName := b.Name
		for _, v := range b.Versions {
			version := v.Number
			total := 0
			for _, r := range v.Regions {
				for _, h := range r.Hosts {
					total += h.Count
				}
			}
			key := getKey(quotaName, browserName, version)
			quota[key] = total
		}
	}
}

func (quota Quota) MaxConnections(quotaName string, browserName string, version string) int {
	key := getKey(quotaName, browserName, version)
	if total, ok := quota[key]; ok {
		return total
	}
	return 0
}

func getKey(quotaName string, browserName string, version string) string {
	return fmt.Sprintf("%s%s%s", quotaName, browserName, version)
}
