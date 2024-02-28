package event_bus

import (
	"sort"
	"sync"
	"log"
)

var EBus = NewEventBus()

type EventBus struct {
	subscribers   map[string][]*Subscriber
	eventHandlers map[string]EventHandler
	subscriberL   sync.RWMutex
	publishL      sync.RWMutex
	mutex         sync.RWMutex
}

type Subscriber struct {
	Name     string
	Ch       chan interface{}
	Quit     chan bool
	Priority int
}

type EventHandler interface {
	HandleEvent(string, *ObjectData)
}

type ObjectData struct {
	ResourceVersion string
	Name            string
	NotificationId  string
}

// Modules will call StartSubscriber to initialise and start listening for event eventType
func (e *EventBus) StartSubscriber(moduleName, eventType string, priority int, eventHandler EventHandler) {
	subscriber := e.Subscribe(moduleName, eventType, priority, eventHandler)

	go func() {
		for {
			select {
			case event := <-subscriber.Ch:
				log.Printf("\nSubscriber %s for %s received \n", moduleName, eventType)

				handlerKey := moduleName + "." + eventType
				if handler, ok := e.eventHandlers[handlerKey]; ok {
					if objectData, ok := event.(*ObjectData); ok {
						handler.HandleEvent(eventType, objectData)
					} else {
						subscriber.Ch <- "error: unexpected event type"
					}
					// handler.HandleEvent(eventType, event)
				} else {
					subscriber.Ch <- "error: no event handler found"
				}
			case <-subscriber.Quit:
				close(subscriber.Ch)
				return
			}
		}
	}()
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers:   make(map[string][]*Subscriber),
		eventHandlers: make(map[string]EventHandler),
	}
}

// Subscribe api provides registration of a subscriber to the given eventType
func (e *EventBus) Subscribe(moduleName, eventType string, priority int, eventHandler EventHandler) *Subscriber {
	e.subscriberL.Lock()
	defer e.subscriberL.Unlock()

	subscriber := &Subscriber{
		Name:     moduleName,
		Ch:       make(chan interface{}, 1),
		Quit:     make(chan bool),
		Priority: priority,
	}

	e.subscribers[eventType] = append(e.subscribers[eventType], subscriber)
	e.eventHandlers[moduleName+"."+eventType] = eventHandler

	// Sort subscribers based on priority
	sort.Slice(e.subscribers[eventType], func(i, j int) bool {
		return e.subscribers[eventType][i].Priority < e.subscribers[eventType][j].Priority
	})

	log.Printf("Subscriber %s registered for event %s with priority %d\n", moduleName, eventType, priority)
	return subscriber
}

// GetSubscribers api is used to fetch the list of subscribers registered with given eventType is priority order
// first in list has the higher priority followed by others and so on
func (e *EventBus) GetSubscribers(eventType string) []*Subscriber {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	return e.subscribers[eventType]
}

// Publish api notifies the subscribers with certain eventType
func (e *EventBus) Publish(objectData *ObjectData, subscriber *Subscriber) {
	e.publishL.RLock()
	defer e.publishL.RUnlock()
	subscriber.Ch <- objectData
}

// Unsubscribex the subscriber, which delete the subscriber(all resourceses will be washed out)
func (e *EventBus) Unsubscribe(subscriber *Subscriber) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	subscriber.Quit <- true
	log.Printf("\nSubscriber %s is unsubscribed for all events\n", subscriber.Name)
}

func (s *Subscriber) Unsubscribe() {
	close(s.Ch)
}

// UnsubscribeEvent, will unsubscribe particular eventType of a subscriber
func (e *EventBus) UnsubscribeEvent(subscriber *Subscriber, eventType string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if subscribers, ok := e.subscribers[eventType]; ok {
		for i, sub := range subscribers {
			if sub == subscriber {
				e.subscribers[eventType] = append(subscribers[:i], subscribers[i+1:]...)
				subscriber.Quit <- true
				log.Printf("\nSubscriber %s is unsubscribed for event %s\n", subscriber.Name, eventType)
				break
			}
		}

		if len(e.subscribers[eventType]) == 0 {
			delete(e.subscribers, eventType)
		}
	}
}
