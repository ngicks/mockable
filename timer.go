package mockable

import (
	"sync"
	"time"
)

type Clock interface {
	Nower
	Timer
}

// The Nower is a mockable interface
// where callers acquire current time by calling Now.
type Nower interface {
	Now() time.Time
}

var _ Nower = (*NowerReal)(nil)

// NowerReal is an implementation of the Nower interface.
// It only wraps time.Now.
type NowerReal struct{}

// Now implements Clock interface.
// Now returns the current local time using runtime timer.
func (_ NowerReal) Now() time.Time {
	return time.Now()
}

var _ Nower = (*NowerFake)(nil)

type NowerFake struct {
	mu      sync.Mutex
	current time.Time
}

func (n *NowerFake) Now() time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.current
}

func (n *NowerFake) SetNow(t time.Time) (prev time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.current, prev = t, n.current
	return prev
}

// The Timer is a mockable interface equivalent to the time.Timer.
//
// Unlike the time.Timer, a New function for the Timer must create it at stopped state.
// Also its Reset method has no return value since it is there only for backward compatibility.
// It will try to Stop the timer before Reset for best effort,
// however still, this does not prevent the race condition of timer expiration.
//
// Use this as an unexported field and swap out in tests.
// In non-test env, TimerReal should suffice. in tests, use FakeTimer or other implementations.
type Timer interface {
	// C is equivalent of timer.C
	C() <-chan time.Time
	// Stop prevents timer from firing. It returns true if it successfully stopped the timer, false if it has already expired or been stopped.
	Stop() bool
	// Reset changes the timer to expire after duration d. Call Reset only for explicitly stopped and drained timer.
	Reset(d time.Duration)
}

var _ Clock = (*ClockReal)(nil)

// ClockReal implements Clock using a runtime timer.
type ClockReal struct {
	T *time.Timer
}

// NewClockReal returns newly created ClockReal.
// This creates stopped timer unlike time.NewTimer.
func NewClockReal() *ClockReal {
	timer := time.NewTimer(30 * 365 * 24 * time.Hour)
	timer.Stop()
	return &ClockReal{
		T: timer,
	}
}

func (c *ClockReal) Now() time.Time {
	return time.Now()
}

func (c *ClockReal) C() <-chan time.Time {
	return c.T.C
}

func (c *ClockReal) Stop() bool {
	return c.T.Stop()
}

func (t *ClockReal) Reset(d time.Duration) {
	if !t.Stop() {
		time.Sleep(time.Nanosecond)
		select {
		case <-t.T.C:
		default:
		}
	}
	t.T.Reset(d)
}

var _ Clock = (*ClockFake)(nil)

type ClockFake struct {
	sync.Mutex
	// current is a mocked current time which will be set
	// and sent through TimeCh by Send method.
	// current can be retrieved also by calling Now().
	//
	// current is used to mock timer's behavior
	// where Timer.C() emits the current time when the timer is expired.
	current time.Time
	TimeCh  chan time.Time
	// The resetArg holds records of Reset calls.
	// Every time Reset is called, resetArg is appended.
	// Stop also appends it with nil.
	resetArg []*time.Duration
	// ResetCh can be used to synchronize to or wait for Reset calls.
	// If an instance is initialized with NewTimerFake, ResetCh is buffered with size of 1.
	ResetCh chan time.Duration
	// StopCh can be used to synchronize to or wait for Stop calls.
	// If an instance is initialized with NewTimerFake, StopCh is buffered with size of 1.
	StopCh chan struct{}
	// sending is a boolean flag represents
	// whether Clock is sending a time value via TimeCh or not.
	sending   bool
	scheduled bool
}

func NewClockFake(current time.Time) *ClockFake {
	return &ClockFake{
		current:  current,
		TimeCh:   make(chan time.Time),
		resetArg: make([]*time.Duration, 0),
		ResetCh:  make(chan time.Duration, 1),
		StopCh:   make(chan struct{}, 1),
	}
}

// Now implements Nower.
func (c *ClockFake) Now() time.Time {
	c.Lock()
	defer c.Unlock()
	return c.current
}

func (c *ClockFake) C() <-chan time.Time {
	return c.TimeCh
}

func (c *ClockFake) Reset(d time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.resetArg = append(c.resetArg, &d)
	c.scheduled = true
	select {
	case <-c.TimeCh:
	default:
	}
	select {
	case c.ResetCh <- d:
	default:
	}
}

// true if it successfully stopped the timer, false if it has already expired or been stopped.
func (c *ClockFake) Stop() bool {
	c.Lock()
	defer c.Unlock()
	c.resetArg = append(c.resetArg, nil)
	select {
	case c.StopCh <- struct{}{}:
	default:
	}
	beenScheduled := c.scheduled
	c.scheduled = false
	return beenScheduled
}

func (c *ClockFake) SetNow(t time.Time) (prev time.Time) {
	c.Lock()
	defer c.Unlock()
	c.current, prev = t, c.current
	return prev
}

// Send sends the time advanced from the Current by the last Reset duration.
// If c is never reset, it behave as it is Reset with 0.
//
// Send keeps invariants where (<-c.C()).Before(c.Now()) is always true
// by stepping the current time slightly forward.
// Taking the time from the runtime must take a few nano seconds.
func (c *ClockFake) Send() (prev time.Time) {
	c.Lock()
	var lastReset time.Duration
	for i := len(c.resetArg); i > 0; i-- {
		arg := c.resetArg[i-1]
		if arg != nil {
			lastReset = *arg
			break
		}
	}
	next := c.current.Add(lastReset)

	prev, c.current = c.current, next.Add(1)
	c.sending = true
	c.Unlock()

	c.TimeCh <- next

	c.Lock()
	c.scheduled = false
	c.sending = false
	c.Unlock()
	return prev
}

// ExhaustCh exhausts ResetCh and StopCh.
func (c *ClockFake) ExhaustCh() {
	for {
		select {
		case <-c.ResetCh:
		case <-c.StopCh:
		default:
			return
		}
	}
}

// CloneResetArg clones t.ResetArg.
func (c *ClockFake) CloneResetArg() []*time.Duration {
	c.Lock()
	defer c.Unlock()

	out := make([]*time.Duration, len(c.resetArg))
	copy(out, c.resetArg)
	return out
}

// LastReset peeks last element of t.ResetArg.
// If t is never Reset, returns false for ok.
func (c *ClockFake) LastReset() (dur time.Duration, ok bool) {
	c.Lock()
	defer c.Unlock()

	for i := len(c.resetArg); i > 0; i-- {
		if c.resetArg[i-1] != nil {
			return *c.resetArg[i-1], true
		}
	}

	return 0, false
}

// IsSending determines t is sending a time value to TimeCh.
// Be cautious that there is always a race condition between channel send and status update.
func (c *ClockFake) IsSending() bool {
	c.Lock()
	defer c.Unlock()
	return c.sending
}

func (c *ClockFake) IsScheduled() bool {
	c.Lock()
	defer c.Unlock()
	return c.scheduled
}
