package clock

import "time"

// Clock provides time.Now() access.
type Clock struct{}

// NowUnix returns current unix seconds.
func (Clock) NowUnix() int64 {
	return time.Now().Unix()
}
