package main

import (
	. "github.com/aandryashin/matchers"
	"net/http"
	"strings"
	"testing"
	"time"
	"github.com/docker/distribution/context"
)

const (
	defaultTimeout = 100 * time.Millisecond
)

var (
	emptyRequest, _ = http.NewRequest(http.MethodGet, "http://example.com/", strings.NewReader("payload"))
)

func TestSize(t *testing.T) {
	queue := CreateQueue(1)
	AssertThat(t, queue.Size(), EqualTo{0})
	lease, _ := queue.Push(emptyRequest.Context())
	AssertThat(t, queue.Size(), EqualTo{1})
	queue.Pop(lease)
	AssertThat(t, queue.Size(), EqualTo{0})
}

func TestCleanupChannels(t *testing.T) {
	queue := CreateQueue(1)
	lease, _ := queue.Push(context.Background())
	queue.SetCapacity(2)
	AssertThat(t, len(queue.channels), EqualTo{2})
	queue.Pop(lease)
	AssertThat(t, len(queue.channels), EqualTo{1})
}

func TestSetCapacity(t *testing.T) {
	queue := CreateQueue(1)
	lease1, _ := queue.Push(emptyRequest.Context())
	queue.SetCapacity(2)
	AssertThat(t, queue.Capacity(), EqualTo{2})
	queue.Push(emptyRequest.Context())
	lease2, _ := queue.Push(emptyRequest.Context())
	AssertThat(t, queue.Size(), EqualTo{3})
	AssertThat(t, actionTimeouts(func() { queue.Push(emptyRequest.Context()) }), EqualTo{true})
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease1) }), EqualTo{false})
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease2) }), EqualTo{false})
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease2) }), EqualTo{false})
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease2) }), EqualTo{false}) //This one is the last push data
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease2) }), EqualTo{true})
}

func TestSetCapacityZeroLength(t *testing.T) {
	queue := CreateQueue(1)
	lease1, _ := queue.Push(emptyRequest.Context())
	queue.Pop(lease1) //There's only one channel in slice but it's already empty and should be deleted
	queue.SetCapacity(2)
	lease2, _ := queue.Push(emptyRequest.Context())
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease2) }), EqualTo{false})
}

func TestPop(t *testing.T) {
	queue := CreateQueue(2)
	lease, _ := queue.Push(emptyRequest.Context())
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease) }), EqualTo{false})
	AssertThat(t, actionTimeouts(func() { queue.Pop(lease) }), EqualTo{true})
}

func actionTimeouts(action func()) bool {
	timeout := make(chan bool, 1)
	ch := make(chan bool)
	go func() {
		action()
		ch <- true
	}()
	go func() {
		time.Sleep(defaultTimeout)
		timeout <- true
	}()
	select {
	case <-ch:
		return false
	case <-timeout:
		return true
	}
}
