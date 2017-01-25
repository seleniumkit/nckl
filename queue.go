package main

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// An extensible fixed size blocking queue based on channels.
// Internally we store a list of channels with fixed size. When pushing an item
// we always add to the last channel (i.e. the newest one). When popping an item
// we use the first channel. We remove channels from the list when they are
// emptied.
type Queue interface {
	Push(context context.Context) (Lease, bool)
	Pop(lease Lease)
	Size() int
	Capacity() int
	SetCapacity(newCapacity int)
	Dump() string
}

type Lease uint64

func CreateQueue(initialCapacity int) *queueImpl {
	ret := &queueImpl{
		channels: make(map[Lease]chan struct{}),
	}
	ret.SetCapacity(initialCapacity)
	return ret
}

type queueImpl struct {
	channels     map[Lease]chan struct{}
	currentLease Lease
	lock         sync.RWMutex
}

func (q *queueImpl) Push(context context.Context) (Lease, bool) {
	q.lock.RLock()
	lease := q.currentLease
	ch := q.channels[lease]
	q.lock.RUnlock()
	select {
	case <-context.Done():
		return 0, true
	case ch <- struct{}{}:
		return lease, false
	}
}

func (q *queueImpl) Pop(lease Lease) {
	q.lock.RLock()
	if ch, ok := q.channels[lease]; ok {
		q.lock.RUnlock()
		<-ch
		q.cleanupChannels()
	} else {
		q.lock.RUnlock()
	}
}

func (q *queueImpl) cleanupChannels() {
	q.lock.Lock()
	defer q.lock.Unlock()
	for lease, ch := range q.channels {
		if lease < q.currentLease && len(ch) == 0 {
			close(ch)
			delete(q.channels, lease)
		}
	}
}

func (q *queueImpl) Size() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	size := 0
	for _, ch := range q.channels {
		size += len(ch)
	}
	return size
}

func (q *queueImpl) Capacity() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return cap(q.channels[q.currentLease])
}

func (q *queueImpl) SetCapacity(newCapacity int) {
	//TODO: we often set 0 and then positive number. This is why a lot of channels exist and effective queue size can be many times greater than its desired capacity
	if len(q.channels) == 0 || q.Capacity() != newCapacity {
		q.lock.Lock()
		q.currentLease++
		q.channels[q.currentLease] = make(chan struct{}, newCapacity)
		q.lock.Unlock()
	}
	q.cleanupChannels()
}

func (q *queueImpl) Dump() string {
	var bb bytes.Buffer
	bb.WriteString(fmt.Sprintf("Queue: cap=%d len=%d currentLease=%d\n", q.Capacity(), q.Size(), q.currentLease))
	for i := 0; i <= int(q.currentLease); i++ {
		lease := Lease(i)
		if ch, ok := q.channels[lease]; ok {
			bb.WriteString(fmt.Sprintf("ch=%d cap=%d len=%d\n", lease, cap(ch), len(ch)))
		}
	}
	return bb.String()
}
