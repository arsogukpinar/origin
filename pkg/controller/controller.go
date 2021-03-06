package controller

import (
	kcache "github.com/GoogleCloudPlatform/kubernetes/pkg/client/cache"
	kutil "github.com/GoogleCloudPlatform/kubernetes/pkg/util"
)

// RunnableController is a controller which implements a Run loop.
type RunnableController interface {
	// Run starts the asynchronous controller loop.
	Run()
}

// RetryController is a RunnableController which delegates resource
// handling to a function and knows how to safely manage retries of a resource
// which failed to be successfully handled.
type RetryController struct {
	// Queue is where work is retrieved for Handle.
	Queue Queue

	// Handle is expected to process the next resource from the queue.
	Handle func(interface{}) error

	// ShouldRetry returns true if the resource and error returned from
	// HandleNext should trigger a retry via the RetryManager.
	ShouldRetry func(interface{}, error) bool

	// RetryManager is fed the handled resource if Handle returns a Retryable
	// error. If Handle returns no error, the RetryManager is asked to forget
	// the resource.
	RetryManager RetryManager
}

// Queue is a narrow abstraction of a cache.FIFO.
type Queue interface {
	Pop() interface{}
	AddIfNotPresent(interface{}) error
}

// Run begins processing resources from Queue asynchronously.
func (c *RetryController) Run() {
	go kutil.Forever(func() { c.handleOne(c.Queue.Pop()) }, 0)
}

// handleOne processes resource with Handle. If Handle returns a retryable
// error, the handled resource is passed to the RetryManager. If no error is
// returned from Handle, the RetryManager is asked to forget the processed
// resource.
func (c *RetryController) handleOne(resource interface{}) {
	err := c.Handle(resource)
	if err != nil {
		if c.ShouldRetry(resource, err) {
			c.RetryManager.Retry(resource)
			return
		}
	}
	c.RetryManager.Forget(resource)
}

// RetryManager knows how to retry processing of a resource, and how to forget
// a resource it may be tracking the state of.
type RetryManager interface {
	// Retry will cause resource processing to be retried (for example, by
	// requeueing resource)
	Retry(resource interface{})

	// Forget will cause the manager to erase all prior knowledge of resource
	// and reclaim internal resources associated with state tracking of
	// resource.
	Forget(resource interface{})
}

// QueueRetryManager retries a resource by re-queueing it into a Queue up to
// MaxRetries number of times.
type QueueRetryManager struct {
	// queue is where resources are re-queued.
	queue Queue

	// keyFunc is used to index resources.
	keyFunc kcache.KeyFunc

	// maxRetries is the total number of attempts to requeue an individual
	// resource before giving up. A value of -1 is interpreted as retry forever.
	maxRetries int

	// retries maps resources to their current retry count.
	retries map[string]int
}

// NewQueueRetryManager safely creates a new QueueRetryManager.
func NewQueueRetryManager(queue Queue, keyFunc kcache.KeyFunc, maxRetries int) *QueueRetryManager {
	return &QueueRetryManager{
		queue:      queue,
		keyFunc:    keyFunc,
		maxRetries: maxRetries,
		retries:    make(map[string]int),
	}
}

// Retry will enqueue resource until maxRetries for that resource has been
// exceeded, at which point resource will be forgotten and no longer retried.
//
// A maxRetries value of -1 is interpreted as retry forever.
func (r *QueueRetryManager) Retry(resource interface{}) {
	id, _ := r.keyFunc(resource)

	if _, exists := r.retries[id]; !exists {
		r.retries[id] = 0
	}
	tries := r.retries[id]

	if tries < r.maxRetries || r.maxRetries == -1 {
		// It's important to use AddIfNotPresent to prevent overwriting newer
		// state in the queue which may have arrived asynchronously.
		r.queue.AddIfNotPresent(resource)
		r.retries[id] = tries + 1
	} else {
		r.Forget(resource)
	}
}

// Forget resets the retry count for resource.
func (r *QueueRetryManager) Forget(resource interface{}) {
	id, _ := r.keyFunc(resource)
	delete(r.retries, id)
}
