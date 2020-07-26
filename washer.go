package main

type Washer struct {
	walker *Walker
}

func NewWasher(walker *Walker) *Washer {
	// TODO: implement - keep constructor simple, so better not complicate logic here(like spinning up
	// goroutines)
	return nil
}

// Next returns the next bookmark whose liveness had been checked, aka "washed" by washer. It returns io.EOF
// when there is no more bookmark left, or a non-EOF error when washer encounter any during washing.
// TODO: implement
func (w *Washer) Next() (*Bookmark, error) { return nil, nil }

// Stop stops washer. A call to Next after calling Stop() returns io.EOF
// TODO: implement
func (w *Washer) Stop() error { return nil }
