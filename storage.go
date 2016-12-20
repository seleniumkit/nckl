package main

import (
	client "github.com/coreos/etcd/clientv3"
	"context"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/prometheus/common/log"
)

type Storage interface {
	AddSession(id string)
	DeleteSession(id string)
	OnSessionDeleted(id string, fn func(string))
	MembersCount() int
	Close()
}

type EtcdStorage struct {
	ctx context.Context
	c    *client.Client
}

func NewEtcdStorage(c *client.Client) *EtcdStorage {
	return &EtcdStorage{context.Background(), c}
}

func (storage *EtcdStorage) MembersCount() int {
	members, err := storage.c.Cluster.MemberList(storage.ctx)
	if (err != nil) {
		return 1
	}
	return len(members.Members)
}

func (storage *EtcdStorage) AddSession(id string) {
	lease, err := storage.c.Grant(storage.ctx, int64(sessionTimeout))
	if (err != nil) {
		log.Fatal(err)
	}
	_, err = storage.c.Put(storage.ctx, id, "", client.WithLease(lease.ID))
	if (err != nil) {
		log.Fatal(err)
	}
}

func (storage *EtcdStorage) DeleteSession(id string) {
	storage.c.Delete(storage.ctx, id)
}

func (storage *EtcdStorage) OnSessionDeleted(id string, fn func(string)) {
	responseChannel := storage.c.Watch(context.Background(), id)
	go func() {
		loop: for response := range responseChannel {
			for _, ev := range response.Events {
				if (ev.Type == mvccpb.DELETE) {
					fn(id)
					break loop
				}
			}
		}
	}()
}

func (storage *EtcdStorage) Close() {
	storage.c.Close()
}