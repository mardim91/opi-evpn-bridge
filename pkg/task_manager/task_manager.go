// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2023 Nordix Foundation.

// Package taskmanager contains the task manager logic
package taskmanager

import (
	"time"

	"github.com/google/uuid"
)

type TaskManager struct {
	taskQueue      *TaskQueue
	taskStatusChan chan *TaskStatus
}

type Task struct {
	name            string
	objectType      string
	resourceVersion string
	sub_index       int
	timer           time.Second
	subs            []*Subscriber
}

type TaskStatus struct {
	name            string
	objectType      string
	resourceVersion string
	notificationId  string
	dropTask        bool
	component       infradb.Component
}

func newTaskManager() *TaskManager {
	return &TaskManager{
		taskQueue:      NewTaskQueue(),
		taskStatusChan: make(chan *TaskStatus),
	}
}

func newTask(name, objectType, resourceVersion string, subs []*Subscriber) *Task {
	return &Task{
		name:            name,
		objectType:      objectType,
		resourceVersion: resourceVersion,
		sub_index:       0,
		subs:            subs,
	}
}

func newTaskStatus(name, objectType, resourceVersion, notificationId string, dropTask bool, component infradb.Component) *TaskStatus {
	return &TaskStatus{
		name:            name,
		objectType:      objectType,
		resourceVersion: resourceVersion,
		notificationId:  notificationId,
		dropTask:        dropTask,
		component:       infradb.Component,
	}
}

func (t *TaskManager) StartTaskManager() {

	go t.processTasks()
}

func (t *TaskManager) CreateTask(name, objectType, resourceVersion string, subs []*Subscriber) {
	task := newTask(name, objectType, resourceVersion, subs)
	// The reason that we use a go routing to enqueue the task is because we do not want the main thread to block
	// if the queue is full but only the go routine to block
	go t.taskQueue.Enqueue(task)

}

func (t *TaskManager) StatusUpdated(name, objectType, resourceVersion, notificationId string, dropTask bool, component infradb.Component) {

	taskStatus := newTaskStatus(name, objectType, resourceVersion, notificationId, dropTask, component)

	t.taskStatusChan <- taskStatus

}

func (t *TaskManager) processTasks() {

	var taskStatus *TaskStatus

	for {
		task := t.taskQueue.Dequeue()
	loopTwo:
		for i, sub := range task.subs[task.sub_index:] {
			// TODO: We need a newObjectData function to create the ObjectData objects
			objectData := &ObjectData{
				name:            task.name,
				resourceVersion: task.resourceVersion,
				// We need this notificationId in order to tell if the status that we got
				// in the taskStatusChan corresponds to the latest notificiation that we have sent or not.
				// (e.g. Maybe you have a timeout on the subscribers and you got the notification after the timeout have passed)
				notificationId: uuid.NewString(),
			}
			subFrame.Publish(objectData, sub)

			//TODO: What do we do if the subscriber is not replying ? I can not wait forever in the channel.
		loopThree:
			for {
				// We have this for loop in order to assert that the taskStatus that recieved from the channel is related to the current task.
				// We do that by checking the notificationId
				// If not we just ignore the taskStatus that we have recieved and loop again.
				select {
				case taskStatus = <-t.taskStatusChan:
					if taskStatus.notificationId == objectData.notificationId {
						break loopThree
					}
				// We need a timeout in case that the subscriber doesn't update the status at all for whatever reason.
				// If that occurs then we just take a note which subscriber need to revisit and we requeue the task without any timer
				case <-time.After(30 * time.Second):
					task.sub_index = i
					go t.taskQueue.Enqueue(task)
					break loopThree
				}
			}

			//This check is needed in order to move to the next task if the status channel has timed out or we need to drop the task in case that
			// the task of the object is refering to an old allready updated object or the object is no longer in the database (has been deleted).
			if taskStatus == nil || taskStatus.dropTask {
				break loopTwo
			}

			switch taskStatus.component.Status {
			case "success":
				continue loopTwo
			default:
				task.sub_index = i
				task.timer = taskStatus.component.timer
				time.AfterFunc(task.timer, func() {
					t.taskQueue.Enqueue(task)
				})
				break loopTwo
			}
		}

	}
}
