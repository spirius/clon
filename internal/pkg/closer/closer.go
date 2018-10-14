package closer

import (
	"sync"
)

// A Closer is an object, which can be used
// for cancelation of multiple go routines.
type Closer struct {
	once sync.Once
	lock sync.Mutex
	ch   chan bool
	err  error
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
