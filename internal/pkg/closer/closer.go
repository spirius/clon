package closer

import (
	"sync"
)

// A Closer is an object, which can be used
// for cancelation of multiple go routines.
type Closer struct {
	once     sync.Once
	lock     sync.Mutex
	closed   bool
	ch       chan bool
	err      error
	children map[*Closer]bool
}

// New creates new Closer.
func New() Closer {
	return Closer{
		ch: make(chan bool),
	}
}

// Close closes the underlying channel,
// therefore all interested goroutines
// can be notified.
// Close is thread-safe and can be called
// multiple times, but only first error
// is kept and returned by Wait method.
func (c *Closer) Close(err error) {
	c.once.Do(func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		c.err = err
		close(c.ch)
		c.closed = true
		c.propagate()
	})
}

// Chan returns a receive channel, which
// is closed when Close method is called.
func (c *Closer) Chan() <-chan bool {
	return c.ch
}

// Wait block current goroutine until
// Close method is called. Wait returns
// error provided to first invocation of
// Close method.
func (c *Closer) Wait() error {
	<-c.ch
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.err
}

func (c *Closer) propagate() {
	for ch := range c.children {
		ch.Close(c.err)
	}
}

// Child creates new child closer.
// When this Closer is closer, it will
// close the child as well.
func (c *Closer) Child() *Closer {
	child := New()
	c.AddChild(&child)
	return &child
}

// AddChild add the child to this closer.
func (c *Closer) AddChild(child *Closer) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.closed {
		child.Close(c.err)
		return
	}
	if c.children == nil {
		c.children = make(map[*Closer]bool)
	}
	if _, ok := c.children[child]; ok {
		return
	}
	c.children[child] = true
}
