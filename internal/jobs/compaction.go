package jobs

import "context"

type Compactor struct{}

func NewCompactor() *Compactor {
	return &Compactor{}
}

func (c *Compactor) RunOnce(context.Context) error {
	return nil
}
