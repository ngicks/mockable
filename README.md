# mockable

The mockable interfaces for Go programming language.

## Timer-related

### Nower

A mockable interface of time.Now().

### Timer

A mockable interface equivalent to time.Timer.

### Clock

Clock is an interface where Nower and Timer are combined.

The timer protocol sends the current time when it is expired. This means Timer
itself is not sufficient to mock timer's behavior.

Clock has an implementation in this repository while Timer does not.
