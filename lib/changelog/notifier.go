package libchangelog

import (
	"sync"
)

// Notifier wakes up whoever is waiting for new change-log entries (ex: SSE streams).
//
// The zero value is ready to use.
//
// Example usage:
//
//	// A writer, after committing new change-log entries:
//	notifier.Notify()
//	
//	// A reader:
//	select {
//	case <-notifier.Wait():
//		// read the new entries
//	case <-ctx.Done():
//		return
//	}
type Notifier struct {
	mutex   sync.Mutex
	channel chan struct{}
}

// Wait returns a channel that is closed the next time [Notifier.Notify] is called.
//
// Call Wait before reading the change log, then wait on the channel — so that an entry committed in between is not missed.
func (receiver *Notifier) Wait() <-chan struct{} {
	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()

	if nil == receiver.channel {
		receiver.channel = make(chan struct{})
	}
	return receiver.channel
}

// Notify wakes up everyone waiting.
func (receiver *Notifier) Notify() {
	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()

	if nil != receiver.channel {
		close(receiver.channel)
	}
	receiver.channel = make(chan struct{})
}
