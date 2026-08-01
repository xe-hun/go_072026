package jobs

import "context"

// Compactor is a placeholder for future change-history compaction. It exists as
// a separate unit so compaction can evolve without changing the worker shape.
type Compactor struct{}

// NewCompactor constructs the compaction job helper.
func NewCompactor() *Compactor {
	return &Compactor{}
}

// RunOnce currently does nothing. A future implementation will remove or archive
// old compacted changes after the retention window.
func (c *Compactor) RunOnce(context.Context) error {
	return nil
}
