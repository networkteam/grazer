package grazer

import (
	"container/heap"
	"sync"
)

func newQueue() *queue {
	q := &queue{
		q:       make(queueItems, 0),
		pathIdx: make(map[string]*queueItem),
	}

	heap.Init(&q.q)

	return q
}

// queue is a special priority queue for revalidations with unique route paths.
type queue struct {
	mx              sync.Mutex
	currentPriority uint64
	q               queueItems
	pathIdx         map[string]*queueItem
}

// enqueue adds the given route paths to the queue.
// The invalidatedRoutePaths are added with a higher priority than allRoutePaths.
func (q *queue) enqueue(invalidatedRoutePaths []string, allRoutePaths []string) {
	q.mx.Lock()
	defer q.mx.Unlock()

	// New invalidation means new priority (less than previous invalidation, but higher than all other route paths)
	q.currentPriority++
	prio := q.currentPriority

	for _, routePath := range invalidatedRoutePaths {
		q._addOrUpdate(routePath, prio)
	}

	for _, routePath := range allRoutePaths {
		q._addOrUpdate(routePath, 0)
	}
}

func (q *queue) pop() *string {
	q.mx.Lock()
	defer q.mx.Unlock()

	if len(q.q) == 0 {
		return nil
	}

	item := heap.Pop(&q.q).(*queueItem)

	delete(q.pathIdx, item.routePath)

	return &item.routePath
}

func (q *queue) _addOrUpdate(routePath string, prio uint64) {
	// Check for an existing item
	existingItem := q.pathIdx[routePath]
	if existingItem != nil {
		// We only need to update priority if the item had a zero priority, and it is non-zero now.
		// If the item already had a non-zero priority, we don't want to reduce the priority
		// (due to next invalidation having an increased priority) or make it zero.
		if existingItem.priority == 0 && prio != 0 {
			existingItem.priority = prio
			heap.Fix(&q.q, existingItem.index)
		}
		return
	}

	item := &queueItem{
		priority:  prio,
		routePath: routePath,
	}
	heap.Push(&q.q, item)
	q.pathIdx[routePath] = item
}

type queueItem struct {
	// the priority of the item in the queue. A lower non-zero value means higher priority - while 0 means no priority.
	priority  uint64
	routePath string
	index     int
}

type queueItems []*queueItem

func (q queueItems) Len() int {
	return len(q)
}

func (q queueItems) Less(i, j int) bool {
	pi := q[i].priority
	pj := q[j].priority

	// Stable sort by route path
	if pi == pj {
		return q[i].routePath < q[j].routePath
	}

	// Sort 0 always last
	if pi == 0 {
		return false
	}
	if pj == 0 {
		return true
	}

	// Lower priority value means higher priority
	return pi < pj
}

func (q queueItems) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *queueItems) Push(x any) {
	n := len(*q)
	item := x.(*queueItem)
	item.index = n
	*q = append(*q, item)
}

func (q *queueItems) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*q = old[0 : n-1]
	return item
}
