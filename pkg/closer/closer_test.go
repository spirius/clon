package closer

import (
	"fmt"
	"sync"
	"testing"
)

func TestCloser1(t *testing.T) {
	var err1 = fmt.Errorf("err1")
	var err2 = fmt.Errorf("err2")
	c := New()
	var g sync.WaitGroup
	g.Add(1)

	var g2 sync.WaitGroup
	g2.Add(2)

	var g3 sync.WaitGroup
	g3.Add(1)

	go func() {
		g.Wait()
		c.Close(err1)
		g2.Done()
		g3.Done()
	}()
	go func() {
		g.Wait()
		g3.Wait()
		c.Close(err2)
		g2.Done()
	}()

	g.Done()
	err := c.Wait()
	if err != err1 {
		t.Fatalf("expecting first error (%s), got (%s)", err1, err)
	}
	g2.Wait()
}

func TestCloser2(t *testing.T) {
	c := New()
	var g sync.WaitGroup
	g.Add(1)
	var g2 sync.WaitGroup
	g2.Add(2)

	go func() {
		g.Wait()
		c.Close(nil)
		g2.Done()
	}()
	go func() {
		g.Wait()
		<-c.Chan()
		g2.Done()
	}()

	g.Done()
	c.Wait()
	g2.Wait()
}
