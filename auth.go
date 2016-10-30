package main

import (
	"encoding/csv"
	"github.com/abbot/go-http-auth"
	"log"
	"os"
	"strings"
)

type PropertiesFile struct {
	auth.File
	Users map[string]string
}

func reloadProperties(h *PropertiesFile) {
	r, err := os.Open(h.Path)
	if err != nil {
		log.Printf("Failed to read users list file [%s]: %v\n", h.Path, err)
		return
	}
	csv_reader := csv.NewReader(r)
	csv_reader.Comma = ','
	csv_reader.Comment = '#'
	csv_reader.TrimLeadingSpace = true

	records, err := csv_reader.ReadAll()
	if err != nil {
		log.Printf("Invalid format of users list file [%s]: %v\n", h.Path, err)
		return
	}
	h.Users = make(map[string]string)
	for _, record := range records {
		userPasswordPair := strings.Split(record[0], ":")
		if len(userPasswordPair) == 2 {
			h.Users[userPasswordPair[0]] = userPasswordPair[1]
		}
	}
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
