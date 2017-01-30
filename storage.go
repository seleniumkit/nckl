package main

import (
	"context"
	client "github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"log"
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
	c   *client.Client
}

func NewEtcdStorage(c *client.Client) *EtcdStorage {
	return &EtcdStorage{context.Background(), c}
}

func (storage *EtcdStorage) MembersCount() int {
	members, err := storage.c.Cluster.MemberList(storage.ctx)
	if err != nil {
		return 1
	}
	return len(members.Members)
}

func (storage *EtcdStorage) AddSession(id string) {
	lease, err := storage.c.Grant(storage.ctx, int64(requestTimeout))
	if err != nil {
		log.Println(err.Error())
		return
	}
	_, err = storage.c.Put(storage.ctx, id, "", client.WithLease(lease.ID))
	if err != nil {
		log.Println(err.Error())
		return
	}
}

func (storage *EtcdStorage) DeleteSession(id string) {
	resp, err := storage.c.Get(storage.ctx, id)
	if err != nil {
		log.Println(err.Error())
		return
	}
	for _, 	ev := range resp.Kvs {
		leaseId := ev.Lease
		if (leaseId == 0) {
			storage.c.Delete(storage.ctx, id)
		} else {
			//Automatically deletes key
			storage.c.Revoke(storage.ctx, client.LeaseID(leaseId))
		}
	}
}

func (storage *EtcdStorage) OnSessionDeleted(id string, fn func(string)) {
	ctx, cancel := context.WithTimeout(storage.ctx, requestTimeout)
	responseChannel := storage.c.Watch(ctx, id)
	go func() {
		for response := range responseChannel {
			if response.Canceled {
				fn(id)
				cancel()
				return
			}
			for _, ev := range response.Events {
				if ev.Type == mvccpb.DELETE {
					fn(id)
					cancel()
					return
				}
			}
		}
	}()
}

func (storage *EtcdStorage) Close() {
	storage.c.Close()
}
