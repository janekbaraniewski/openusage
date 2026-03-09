package core

import "time"

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}

type FuncClock func() time.Time

func (f FuncClock) Now() time.Time {
	if f == nil {
		return time.Now()
	}
	return f()
}
