// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
package event_bus

import (
	"sync"
)

type EventBus struct {
	subscribers map[string][]*Subscriber
	mutex       sync.RWMutex
}

type Subscriber struct {
	Ch   chan interface{}
	Quit chan bool
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]*Subscriber),
	}
}

func (e *EventBus) Subscribe(eventType string) *Subscriber {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	subscriber := &Subscriber{
		Ch:   make(chan interface{}),
		Quit: make(chan bool),
	}

	e.subscribers[eventType] = append(e.subscribers[eventType], subscriber)

	return subscriber
}

func (e *EventBus) Publish(eventType string, data interface{}) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	subscribers, ok := e.subscribers[eventType]
	if !ok {
		return
	}

	for _, sub := range subscribers {
		sub.Ch <- data
	}
}

func (s *Subscriber) Unsubscribe() {
	close(s.Ch)
	s.Quit <- true
}
