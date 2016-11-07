package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"github.com/abbot/go-http-auth"
	"log"
	"os"
	"strings"
	"sync"
)

type PropertiesFile struct {
	auth.File
	Users map[string]string
}

var (
	authLock sync.Mutex
)

func reloadProperties(h *PropertiesFile) {
	authLock.Lock()
	defer authLock.Unlock()
	log.Printf("loading users from [%s]\n", h.Path)
	r, err := os.Open(h.Path)
	if err != nil {
		log.Printf("failed to read users from [%s]: %v\n", h.Path, err)
		return
	}
	csv_reader := csv.NewReader(r)
	csv_reader.Comma = ','
	csv_reader.Comment = '#'
	csv_reader.TrimLeadingSpace = true

	records, err := csv_reader.ReadAll()
	if err != nil {
		log.Printf("invalid format of users file [%s]: %v\n", h.Path, err)
		return
	}
	h.Users = make(map[string]string)
	for _, record := range records {
		userPasswordPair := strings.Split(record[0], ":")
		if len(userPasswordPair) == 2 {
			h.Users[userPasswordPair[0]] = base64SHA1(userPasswordPair[1])
		}
	}
}

func base64SHA1(s string) string {
	d := sha1.New()
	d.Write([]byte(s))
	return fmt.Sprintf("{SHA}%s", base64.StdEncoding.EncodeToString(d.Sum(nil)))
}

func PropertiesFileProvider(filename string) auth.SecretProvider {
	h := &PropertiesFile{File: auth.File{Path: filename}}
	h.Reload = func() {
		reloadProperties(h)
	}
	return func(user, realm string) string {
		h.ReloadIfNeeded()
		password, exists := h.Users[user]
		if !exists {
			return ""
		}
		return password
	}
}
