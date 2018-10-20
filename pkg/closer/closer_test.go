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

func TestCloserChild1(t *testing.T) {
	c := New()
	child1 := c.Child()
	child1_1 := child1.Child()

	var gw1, gw2 sync.WaitGroup
	gw1.Add(1)
	gw2.Add(1)

	go func() {
		gw1.Done()
		<-child1_1.Chan()
		gw2.Done()
	}()

	gw1.Wait()
	c.Close(nil)
	gw2.Wait()
}

func TestCloserChild2(t *testing.T) {
	c := New()
	child1 := c.Child()
	child1_1 := child1.Child()

	var gw1, gw2 sync.WaitGroup
	gw1.Add(1)
	gw2.Add(1)

	go func() {
		gw1.Done()
		<-child1_1.Chan()
		gw2.Done()
	}()

	gw1.Wait()
	child1.Close(nil)
	gw2.Wait()
}

func TestCloserChild_alreadyClosedParent(t *testing.T) {
	c := New()
	c.Close(nil)
	child1 := c.Child()
	<-child1.Chan()
}

func TestCloserChild_childSameAsParent(t *testing.T) {
	c := New()
	c.AddChild(c)
	c.Close(nil)
	<-c.Chan()
}

func TestCloserChild_alreadyChild(t *testing.T) {
	c := New()
	child := c.Child()
	c.AddChild(child)
	c.Close(nil)
	<-c.Chan()
}

func TestCloserChild_multipleBranches(t *testing.T) {
	c := New()
	child1 := c.Child()
	child2 := c.Child()
	child2_1 := child2.Child()

	child2.Close(nil)
	<-child2_1.Chan()

	select {
	case <-c.Chan():
		t.Fatalf("parent closer shouldn't be closed")
	case <-child1.Chan():
		t.Fatalf("child1 closer shouldn't be closed")
	default:
	}
}
