package notes

import "errors"

// ErrSnapshotNotFound is reserved for service-level snapshot errors. Current
// store calls map missing snapshots directly to the common API not-found error.
var ErrSnapshotNotFound = errors.New("snapshot not found")
