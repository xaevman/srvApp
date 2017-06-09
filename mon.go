package srvApp

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xaevman/ini"
)

const (
	monDefaultSrvAddr  = "mon.undeadlabs.net"
	monDefaultSrvPort  = 80
	monDefaultWaitMins = 60
)

// monitor module goroutine sync
var (
	monShutdownChan = make(chan interface{}, 0)
)

// monitor module config synchronization
var (
	monCfgLock  sync.RWMutex
	monUri      string
	monWaitMins int
)

func monInit() {
	defer ini.Subscribe(appConfig, monOnConfigChange)
	go monAsyncWork()
}

func monShutdown() {
	close(monShutdownChan)
}

func monOnConfigChange(cfg *ini.IniCfg, changeCount int) {
	monCfgLock.Lock()
	defer monCfgLock.Unlock()

	srvLog.Info("monOnConfigChange")

	section := cfg.GetSection("net")

	val := section.GetFirstVal("MonitorSrvAddr")
	monAddr := val.GetValStr(0, monDefaultSrvAddr)

	val = section.GetFirstVal("MonitorSrvPort")
	monPort := val.GetValInt(0, monDefaultSrvPort)

	monUri = fmt.Sprintf("http://%s:%d/register", monAddr, monPort)
	srvLog.Info("MonitorSrvAddr: %s", monUri)

	val = section.GetFirstVal("MonitorReportIntervalMins")
	monWaitMins = val.GetValInt(0, monDefaultWaitMins)
	srvLog.Info("MonitorReportIntervalMins: %d", monWaitMins)
}

func monAsyncWork() {
	monCallHome()

	var waitMins int

	for {
		monCfgLock.RLock()
		waitMins = monWaitMins
		monCfgLock.RUnlock()

		select {
		case <-monShutdownChan:
			return
		case <-time.After(time.Duration(waitMins) * time.Minute):
			monCallHome()
		}
	}
}

func monCallHome() {
	monCfgLock.RLock()

	_, err := http.Get(monUri)
	if err != nil {
		Log().Error("Error reporting status to monitoring service: %v", err)
	}

	monCfgLock.RUnlock()
}
