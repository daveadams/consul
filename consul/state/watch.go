package state

import (
	"sync"

	"github.com/armon/go-radix"
)

// Watch is the external interface that's common to all the different flavors.
type Watch interface {
	// Wait registers the given channel and calls it back when the watch
	// fires.
	Wait(notifyCh chan struct{})

	// Clear deregisters the given channel.
	Clear(notifyCh chan struct{})
}

// FullTableWatch implements a single notify group for a table.
type FullTableWatch struct {
	group NotifyGroup
}

// NewFullTableWatch returns a new full table watch.
func NewFullTableWatch() *FullTableWatch {
	return &FullTableWatch{}
}

// See Watch.
func (w *FullTableWatch) Wait(notifyCh chan struct{}) {
	w.group.Wait(notifyCh)
}

// See Watch.
func (w *FullTableWatch) Clear(notifyCh chan struct{}) {
	w.group.Clear(notifyCh)
}

// Notify wakes up all the watchers registered for this table.
func (w *FullTableWatch) Notify() {
	w.group.Notify()
}

// DumbWatchManager is a wrapper that allows nested code to arm full table
// watches multiple times but fire them only once.
type DumbWatchManager struct {
	tableWatches map[string]*FullTableWatch
	armed        map[string]bool
}

// NewDumbWatchManager returns a new dumb watch manager.
func NewDumbWatchManager(tableWatches map[string]*FullTableWatch) *DumbWatchManager {
	return &DumbWatchManager{
		tableWatches: tableWatches,
		armed:        make(map[string]bool),
	}
}

// Arm arms the given table's watch.
func (d *DumbWatchManager) Arm(table string) {
	if _, ok := d.armed[table]; !ok {
		d.armed[table] = true
	}
}

// Notify fires watches for all the armed tables.
func (d *DumbWatchManager) Notify() {
	for table, _ := range d.armed {
		d.tableWatches[table].Notify()
	}
}

// PrefixWatch maintains a notify group for each prefix, allowing for much more
// fine-grained watches.
type PrefixWatch struct {
	// watches has the set of notify groups, organized by prefix.
	watches *radix.Tree

	// lock protects the watches tree.
	lock sync.Mutex
}

// NewPrefixWatch returns a new prefix watch.
func NewPrefixWatch() *PrefixWatch {
	return &PrefixWatch{watches: radix.New()}
}

// GetSubwatch returns the notify group for the given prefix.
func (w *PrefixWatch) GetSubwatch(prefix string) *NotifyGroup {
	w.lock.Lock()
	defer w.lock.Unlock()

	if raw, ok := w.watches.Get(prefix); ok {
		return raw.(*NotifyGroup)
	}

	group := &NotifyGroup{}
	w.watches.Insert(prefix, group)
	return group
}

// Notify wakes up all the watchers associated with the given prefix. If subtree
// is true then we will also notify all the tree under the prefix, such as when
// a key is being deleted.
func (w *PrefixWatch) Notify(prefix string, subtree bool) {
	w.lock.Lock()
	defer w.lock.Unlock()

	var cleanup []string
	fn := func(k string, v interface{}) bool {
		group := v.(*NotifyGroup)
		group.Notify()
		if k != "" {
			cleanup = append(cleanup, k)
		}
		return false
	}

	// Invoke any watcher on the path downward to the key.
	w.watches.WalkPath(prefix, fn)

	// If the entire prefix may be affected (e.g. delete tree),
	// invoke the entire prefix.
	if subtree {
		w.watches.WalkPrefix(prefix, fn)
	}

	// Delete the old notify groups.
	for i := len(cleanup) - 1; i >= 0; i-- {
		w.watches.Delete(cleanup[i])
	}
}