package srvApp

import (
    "sync"
)

var (
    shutdownSubs = make([]func(), 0)
    shutdownLock sync.RWMutex
)

// Shutdown notify allows a srvApp implementation to subcribe to the 
// application shutdown event.
func ShutdownNotify(f func()) {
    shutdownLock.Lock()
    defer shutdownLock.Unlock()

    shutdownSubs = append(shutdownSubs, f)
}

// NotifyShutdown loops through the list of shutdown subscriber functions,
// calling them all in the order they were added.
func NotifyShutdown() {
    shutdownLock.RLock()
    defer shutdownLock.RUnlock()

    for i := range shutdownSubs {
        shutdownSubs[i]()
    }
}
