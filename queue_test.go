package main

import (
	"github.com/aandryashin/matchers"
	"testing"
	"time"
)

const defaultTimeout = 100 * time.Millisecond

func TestSize(t *testing.T) {
	queue := CreateQueue(1)
	matchers.AssertThat(t, queue.Size(), matchers.EqualTo{0})
	queue.Push()
	matchers.AssertThat(t, queue.Size(), matchers.EqualTo{1})
	queue.Pop()
	matchers.AssertThat(t, queue.Size(), matchers.EqualTo{0})
}

func TestSetCapacity(t *testing.T) {
	queue := CreateQueue(1)
	queue.Push()
	queue.SetCapacity(2)
	queue.Push()
	queue.Push()
	matchers.AssertThat(t, queue.Size(), matchers.EqualTo{3})
	matchers.AssertThat(t, actionTimeouts(queue.Push), matchers.EqualTo{true})
}

func TestPop(t *testing.T) {
	queue := CreateQueue(2)
	queue.Push()
	matchers.AssertThat(t, actionTimeouts(queue.Pop), matchers.EqualTo{false})
	matchers.AssertThat(t, actionTimeouts(queue.Pop), matchers.EqualTo{true})
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
