package main

// An extensible fixed size blocking queue based on channels.
// Internally we store a list of channels with fixed size. When pushing an item
// we always add to the last channel (i.e. the newest one). When popping an item
// we use the first channel. We remove channels from the list when they are
// emptied.
type Queue interface {
	Push()
	Pop()
	Size() int
	SetCapacity(newCapacity int)
}

func CreateQueue(initialCapacity int) *queueImpl {
	var channels []chan struct{}
	channels = append(channels, make(chan struct{}, initialCapacity))
	return &queueImpl{
		channels: channels,
	}
}

type queueImpl struct {
	channels []chan struct{}
}

func (q *queueImpl) Push() {
	q.channels[len(q.channels)-1] <- struct{}{}
}

func (q *queueImpl) Pop() {
	<-q.channels[0]
	if len(q.channels) > 1 && len(q.channels[0]) == 0 {
		close(q.channels[0])
		q.channels = q.channels[1:]
	}
}

func (q *queueImpl) Size() int {
	size := 0
	for _, ch := range q.channels {
		size += len(ch)
	}
	return size
}

func (q *queueImpl) SetCapacity(newCapacity int) {
	q.channels = append(q.channels, make(chan struct{}, newCapacity))
}
