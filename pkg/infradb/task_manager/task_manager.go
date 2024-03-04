// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Nordix Foundation.

// Package taskmanager contains the task manager logic
package task_manager

import (
	"github.com/google/uuid"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"log"
	"time"

	// Typo
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
)

var TaskMan = newTaskManager()

type TaskManager struct {
	taskQueue      *TaskQueue
	taskStatusChan chan *TaskStatus
}

type Task struct {
	name            string
	objectType      string
	resourceVersion string
	sub_index       int
	retryTimer      time.Duration
	subs            []*event_bus.Subscriber
}

type TaskStatus struct {
	name            string
	objectType      string
	resourceVersion string
	notificationId  string
	dropTask        bool
	component       *common.Component
}

func newTaskManager() *TaskManager {
	return &TaskManager{
		taskQueue:      NewTaskQueue(),
		taskStatusChan: make(chan *TaskStatus),
	}
}

func newTask(name, objectType, resourceVersion string, subs []*event_bus.Subscriber) *Task {
	return &Task{
		name:            name,
		objectType:      objectType,
		resourceVersion: resourceVersion,
		sub_index:       0,
		subs:            subs,
	}
}

func newTaskStatus(name, objectType, resourceVersion, notificationId string, dropTask bool, component *common.Component) *TaskStatus {
	return &TaskStatus{
		name:            name,
		objectType:      objectType,
		resourceVersion: resourceVersion,
		notificationId:  notificationId,
		dropTask:        dropTask,
		component:       component,
	}
}

func (t *TaskManager) StartTaskManager() {
	go t.processTasks()
	log.Println("Task Manager has started")
}

func (t *TaskManager) CreateTask(name, objectType, resourceVersion string, subs []*event_bus.Subscriber) {
	task := newTask(name, objectType, resourceVersion, subs)
	// The reason that we use a go routing to enqueue the task is because we do not want the main thread to block
	// if the queue is full but only the go routine to block
	go t.taskQueue.Enqueue(task)
	log.Printf("CreateTask(): New Task has been created: %+v\n", task)
}

func (t *TaskManager) StatusUpdated(name, objectType, resourceVersion, notificationId string, dropTask bool, component *common.Component) {
	taskStatus := newTaskStatus(name, objectType, resourceVersion, notificationId, dropTask, component)

	// Do we need to make this call happen in a goroutine in order to not make the
	// StatusUpdated function stuck in case that nobody reads what is written on the channel ?
	// Is there any case where this can happen
	// (nobody reads what is written on the channel and the StatusUpdated gets stuck) ?
	t.taskStatusChan <- taskStatus
	log.Printf("StatusUpdated(): New Task Status has been created and sent to channel: %+v\n", taskStatus)
}

func (t *TaskManager) processTasks() {
	var taskStatus *TaskStatus

	for {
		task := t.taskQueue.Dequeue()
		log.Printf("processTasks(): Task has been dequeued for processing: %+v\n", task)

		subsToIterate := task.subs[task.sub_index:]
	loopTwo:
		for i, sub := range subsToIterate {
			// TODO: We need a newObjectData function to create the ObjectData objects
			objectData := &event_bus.ObjectData{
				Name:            task.name,
				ResourceVersion: task.resourceVersion,
				// We need this notificationId in order to tell if the status that we got
				// in the taskStatusChan corresponds to the latest notificiation that we have sent or not.
				// (e.g. Maybe you have a timeout on the subscribers and you got the notification after the timeout have passed)
				NotificationId: uuid.NewString(),
			}
			event_bus.EBus.Publish(objectData, sub)
			log.Printf("processTasks(): Notification has been sent to subscriber %+v with data %+v\n", sub, objectData)

		loopThree:
			for {
				// We have this for loop in order to assert that the taskStatus that received from the channel is related to the current task.
				// We do that by checking the notificationId
				// If not we just ignore the taskStatus that we have received and loop again.
				taskStatus = nil
				select {
				case taskStatus = <-t.taskStatusChan:

					log.Printf("processTasks(): Task Status has been received from the channel %+v\n", taskStatus)
					if taskStatus.notificationId == objectData.NotificationId {
						log.Printf("processTasks(): received notification id %+v equals the sent notification id %+v\n", taskStatus.notificationId, objectData.NotificationId)
						break loopThree
					}
					log.Printf("processTasks(): received notification id %+v doesn't equal the sent notification id %+v\n", taskStatus.notificationId, objectData.NotificationId)

				// We need a timeout in case that the subscriber doesn't update the status at all for whatever reason.
				// If that occurs then we just take a note which subscriber need to revisit and we requeue the task without any timer
				case <-time.After(30 * time.Second):
					log.Printf("processTasks(): No task status has been received in the channel from subscriber %+v. The task %+v will be requeued immediately Task Status %+v\n", sub, task, taskStatus)
					task.sub_index = task.sub_index + i
					go t.taskQueue.Enqueue(task)
					break loopThree
				}
			}

			// This check is needed in order to move to the next task if the status channel has timed out or we need to drop the task in case that
			// the task of the object is referring to an old already updated object or the object is no longer in the database (has been deleted).
			if taskStatus == nil || taskStatus.dropTask {
				log.Println("processTasks(): Move to the next Task in the queue")
				break loopTwo
			}

			switch taskStatus.component.CompStatus {
			case common.COMP_STATUS_SUCCESS:
				log.Printf("processTasks(): Subscriber %+v has processed the task %+v successfully\n", sub, task)
				continue loopTwo
			default:
				log.Printf("processTasks(): Subscriber %+v has not processed the task %+v successfully\n", sub, task)
				task.sub_index = task.sub_index + i
				task.retryTimer = taskStatus.component.Timer
				log.Printf("processTasks(): The Task will be requeued after %+v\n", task.retryTimer)
				time.AfterFunc(task.retryTimer, func() {
					t.taskQueue.Enqueue(task)
				})
				break loopTwo
			}
		}
	}
}
