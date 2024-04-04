// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.

// Package eventbus handles pub sub
package eventbus

import (
	"sync"
)

// EventBus holds the event bus info
type EventBus struct {
	subscribers map[string][]*Subscriber
	mutex       sync.RWMutex
}

// Subscriber holds the info for each subscriber
type Subscriber struct {
	Ch   chan interface{}
	Quit chan bool
}

// NewEventBus initializes ann EventBus object
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]*Subscriber),
	}
}

// Subscribe api provides registration of a subscriber to the given eventType
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

// Publish api notifies the subscribers with certain eventType
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

// Unsubscribe the subscriber, which delete the subscriber(all resources will be washed out)
func (s *Subscriber) Unsubscribe() {
	close(s.Ch)
	s.Quit <- true
}
