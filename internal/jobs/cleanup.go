package jobs

import "context"

type Cleaner struct{}

func NewCleaner() *Cleaner {
	return &Cleaner{}
}

func (c *Cleaner) RunOnce(context.Context) error {
	return nil
}
