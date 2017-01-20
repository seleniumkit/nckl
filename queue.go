package main

import (
	"sync"
	"bytes"
	"fmt"
	"context"
)

// An extensible fixed size blocking queue based on channels.
// Internally we store a list of channels with fixed size. When pushing an item
// we always add to the last channel (i.e. the newest one). When popping an item
// we use the first channel. We remove channels from the list when they are
// emptied.
type Queue interface {
	Push(context context.Context) bool
	Pop()
	Size() int
	Capacity() int
	SetCapacity(newCapacity int)
	Dump() string
}

func CreateQueue(initialCapacity int) *queueImpl {
	var channels []chan struct{}
	ret := &queueImpl{
		channels: channels,
	}
	ret.SetCapacity(initialCapacity)
	return ret
}

type queueImpl struct {
	channels []chan struct{}
	lock     sync.RWMutex
}

func (q *queueImpl) Push(context context.Context) bool {
	q.lock.RLock()
	ch := q.channels[len(q.channels)-1]
	q.lock.RUnlock()
	select {
	case <-context.Done(): return true
	case ch <- struct{}{}: return false
	}
}

func (q *queueImpl) Pop() {
	q.lock.RLock()
	ch := q.channels[0]
	q.lock.RUnlock()
	<-ch
	q.cleanupChannels()
}

func (q *queueImpl) cleanupChannels() {
	q.lock.Lock()
	defer q.lock.Unlock()
	if len(q.channels) > 1 && len(q.channels[0]) == 0 {
		close(q.channels[0])
		q.channels = q.channels[1:]
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
	return cap(q.channels[len(q.channels)-1])
}

func (q *queueImpl) SetCapacity(newCapacity int) {
	//TODO: we often set 0 and then positive number. This is why a lot of channels exist and effective queue size can be many times greater than its desired capacity
	if len(q.channels) == 0 || q.Capacity() != newCapacity {
		q.lock.Lock()
		q.channels = append(q.channels, make(chan struct{}, newCapacity))
		q.lock.Unlock()
	}
	q.cleanupChannels()
}

func (q *queueImpl) Dump() string {
	var bb bytes.Buffer
	bb.WriteString(fmt.Sprintf("Queue: cap=%d len=%d\n", q.Capacity(), q.Size()));
	for i, ch := range q.channels {
		bb.WriteString(fmt.Sprintf("ch=%d cap=%d len=%d\n", i, cap(ch), len(ch)));
	}
	return bb.String();
}
