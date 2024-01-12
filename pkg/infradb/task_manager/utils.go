package task_manager

type TaskQueue struct {
	channel chan *Task
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		channel: make(chan *Task, 200),
	}
}

func (q *TaskQueue) Enqueue(task *Task) {
	q.channel <- task
}

func (q *TaskQueue) Dequeue() *Task {
	return <-q.channel
}

func (q *TaskQueue) Close() {
	close(q.channel)
}
