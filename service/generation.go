package service

import "time"

// generationNow is the queue clock: UTC (SQLite compares times as text) truncated to the millisecond MySQL keeps.
func generationNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}
