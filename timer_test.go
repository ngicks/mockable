package mockable_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ngicks/mockable"
	"github.com/stretchr/testify/require"
)

func TestNowerReal(t *testing.T) {
	require := require.New(t)
	n := mockable.NowerReal{}
	require.Less(time.Since(n.Now()), time.Second)
}

func TestNowerFake(t *testing.T) {
	require := require.New(t)

	n := mockable.NowerFake{}

	equal := func(x, y time.Time) func() bool {
		return func() bool {
			return x.Equal(y)
		}
	}

	require.Condition(equal(n.Now(), time.Time{}))

	now := time.Now()
	require.Equal(time.Time{}, n.SetNow(now))
	require.Condition(equal(now, n.Now()))

	prev, now := now, now.Add(time.Duration(rand.Int63()))
	require.Equal(prev, n.SetNow(now))
	require.Condition(equal(now, n.Now()))
}

func TestClockReal(t *testing.T) {
	require := require.New(t)

	c := mockable.NewClockReal()

	require.False(c.Stop())

	select {
	case <-c.C():
		t.Fatal("C is received without Reset")
	default:
	}

	c.Reset(1)
	then := <-c.C()
	require.GreaterOrEqual(time.Now(), then)

	c.Reset(time.Minute)
	require.True(c.Stop())

	c.Reset(0)
	time.Sleep(time.Millisecond)
	require.False(c.Stop())
	require.False(c.Stop())
}

func TestClockFake(t *testing.T) {
	require := require.New(t)

	now := time.Now()

	c := mockable.NewClockFake(now)
	require.Equal(now, c.Now())

	next := now.AddDate(0, 2, 1).Add(time.Duration(2183012))
	require.Equal(now, c.SetNow(next))
	require.Equal(next, c.Now())

	require.Equal("", cmp.Diff(c.CloneResetArg(), []*time.Duration{}))

	send := func() (switchCh chan struct{}) {
		currentTime := c.Now()
		var prev time.Time

		switchCh = make(chan struct{})
		go func() {
			<-switchCh

			// FailNow is implemented using runtime.Goexit().
			// Effectively preventing it from failing if it is called in a new goroutine.
			// Now let it panic for simplicity reasons.
			normalReturn := false
			defer func() {
				if !normalReturn {
					panic(fmt.Errorf("not equal: currentTime = %s, prev = %s", currentTime, prev))
				}
			}()

			prev = c.Send()

			require.Equal(currentTime, prev)
			normalReturn = true
			close(switchCh)
		}()
		switchCh <- struct{}{}

		for !c.IsSending() {
			time.Sleep(time.Microsecond)
		}

		return switchCh
	}

	testSend := func() {
		switchCh := send()
		require.True(c.IsSending())

		require.True((<-c.C()).Before(c.Now()))
		<-switchCh

		require.False(c.IsSending())
	}

	testSend()
	testSend()

	resetReceived := channelReceived(c.ResetCh)
	stopReceived := channelReceived(c.StopCh)

	require.False(resetReceived())
	require.False(stopReceived())

	require.False(c.Stop())
	require.False(resetReceived())
	require.True(stopReceived())
	lastReset, lastResetOk := c.LastReset()
	require.False(lastResetOk)
	require.Equal(time.Duration(0), lastReset)

	c.Reset(time.Second)
	require.True(resetReceived())
	require.False(stopReceived())

	prev := c.Now()
	testSend()
	current := c.Now()
	require.Greater(current, prev.Add(time.Second))

	lastReset, lastResetOk = c.LastReset()
	require.True(lastResetOk)
	require.Equal(time.Second, lastReset)

	require.False(c.Stop())
	// Stop is ignored by LastReset.
	lastReset, lastResetOk = c.LastReset()
	require.True(lastResetOk)
	require.Equal(time.Second, lastReset)

	c.ExhaustCh()
	require.False(resetReceived())
	require.False(stopReceived())

	c.Reset(3 * time.Second)
	require.True(c.Stop())
	switchCh := send()
	require.False(c.Stop())
	<-c.C()
	<-switchCh

	sec := time.Second
	threeSec := 3 * time.Second

	diff := cmp.Diff(c.CloneResetArg(), []*time.Duration{nil, &sec, nil, &threeSec, nil, nil})
	require.True(diff == "", "diff = %s", diff)
}

func channelReceived[T any](ch chan T) func() bool {
	return func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}
}
