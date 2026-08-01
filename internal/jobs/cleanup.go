package jobs

import "context"

// Cleaner is a placeholder for retention cleanup work such as old tombstones or
// completed outbox rows.
type Cleaner struct{}

// NewCleaner constructs the cleanup job helper.
func NewCleaner() *Cleaner {
	return &Cleaner{}
}

// RunOnce currently does nothing. A future implementation will enforce
// configured retention policies.
func (c *Cleaner) RunOnce(context.Context) error {
	return nil
}
