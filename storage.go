package main

import (
	"github.com/coreos/etcd/client"
	"context"
	"time"
)

type Storage interface {
	AddSession(id string)
	DeleteSession(id string)
	OnSessionDeleted(id string, fn func(string))
	MembersCount() int
}

type EtcdStorage struct {
	ctx context.Context
	keys client.KeysAPI
	members client.MembersAPI
}

func NewEtcdStorage(c client.Client) *EtcdStorage {
	return &EtcdStorage{context.Background(), client.NewKeysAPI(c), client.NewMembersAPI(c)}
}

func (storage *EtcdStorage) MembersCount() int {
	members, err := storage.members.List(storage.ctx)
	if (err != nil) {
		return 1
	}
	return len(members)
}

func (storage *EtcdStorage) AddSession(id string) {
	storage.keys.Set(storage.ctx, id, "", &client.SetOptions{TTL: time.Duration(sessionTimeout) * time.Second})
}

func (storage *EtcdStorage) DeleteSession(id string) {
	storage.keys.Delete(storage.ctx, id, nil)
}

func (storage *EtcdStorage) OnSessionDeleted(id string, fn func(string)) {
	watcher := storage.keys.Watcher(id, nil)
	go func() {
		for {
			response, err := watcher.Next(storage.ctx)
			if (err != nil) {
				break
			} else if (response.Action == "delete") {
				fn(id)
				break
			}
		}
	}()
}