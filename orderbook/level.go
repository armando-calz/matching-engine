package orderbook

// restingOrder is an order waiting in the book. It is a node of its price level's
// queue, so that a cancel can unlink it in O(1) once it is found by id.
type restingOrder struct {
	Order
	level      *level
	prev, next *restingOrder
}

// level holds every resting order at one price, in arrival order (FIFO):
// at equal prices, the order that arrived first is matched first.
type level struct {
	price      Price
	quantity   Quantity // sum of the remaining quantity of the orders in the queue
	count      int
	head, tail *restingOrder
}

func (l *level) pushBack(o *restingOrder) {
	o.level = l
	o.prev = l.tail
	if l.tail != nil {
		l.tail.next = o
	} else {
		l.head = o
	}
	l.tail = o
	l.quantity += o.Quantity
	l.count++
}
