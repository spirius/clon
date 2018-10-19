package closer

import (
	"fmt"
	"sync"
	"time"
)

func ExampleCloser() {
	var (
		wg sync.WaitGroup
		c  = New()
		n  = 0
	)
	wg.Add(5)

	go func() {
		// Wait until other goroutine is counting
		wg.Wait()

		c.Close(fmt.Errorf("done"))
	}()

	go func() {
		for {
			// do some actions here
			select {
			case <-c.Chan():
				return
			case <-time.After(1):
			}
			if n < 5 {
				n++
				wg.Done()
			}
		}
	}()

	err := c.Wait()
	fmt.Printf("received: %s, count: %d\n", err, n)
	// Output: received: done, count: 5
}
