package srvApp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xaevman/app"
	"github.com/xaevman/crash"
	"github.com/xaevman/ini"
	"github.com/xaevman/log"
	"github.com/xaevman/shutdown"
	"github.com/xaevman/trace"
)

const monBadNetIdTrace = "MonBadNetId"

const (
	monDefaultSrvAddr = "mon.undeadlabs.net"
	monDefaultSrvPort = 8100
)

const (
	MonMsgAppInfo = iota
	MonMsgOnLog
	MonMsgCounters
	MonMsgIpsStats
	MonMsgOnIpsReq
)

// monitor module goroutine sync
var (
	_monInitialized   = false
	_monShutdown      = shutdown.New()
	monDisconnectChan = make(chan interface{}, 0)
	monSendQueue      = make(chan *MonUpdateMsg, 0)
)

// monitor module config synchronization
var (
	monCfgLock sync.RWMutex
	monConn    *websocket.Conn
	monKeyStr  string
	monUri     string
)

type MonUpdateMsg struct {
	MsgType     byte
	ServiceName string
	NetId       string
	Data        []byte
}

func monInit() {
	srvLog.Info("mon Init")

	monKeyStr = GetMonKeyStr()

	ini.Subscribe(appConfig, monOnConfigChange)

	httpSrv.RegisterHandler(
		"/cmd/mon/status/",
		monOnStatusCmdUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	go func() {
		defer crash.HandleAll()
		monAsyncWork()
	}()

	go func() {
		defer crash.HandleAll()

		for {
			select {
			case <-_monShutdown.Signal:
				_monInitialized = false
				return
			case msg := <-monSendQueue:
				if monConn == nil {
					<-time.After(1 * time.Second)
					monSend(msg)
				} else {
					err := monConn.WriteJSON(msg)
					if err != nil {
						if msg.MsgType == MonMsgOnLog {
							Log().ErrorLocal("Error sending data JSON: %v", err)
						} else {
							Log().Error("Error sending data JSON: %v", err)
						}
					}
				}
			}
		}
	}()

	_monInitialized = true
}

func monSend(msg *MonUpdateMsg) {
	if !_monInitialized {
		return
	}

	if msg.NetId == "" {
		trace.Log(monBadNetIdTrace, msg)
		return
	}

	go func() {
		defer crash.HandleAll()

		select {
		case <-_monShutdown.Signal:
		case monSendQueue <- msg:
		case <-time.After(20 * time.Second):
			Log().Error("Timeout sending msg of type %d", msg.MsgType)
		}
	}()
}

func monShutdown() {
	srvLog.Info("mon Shutdown")

	_monShutdown.Start()
	if _monShutdown.WaitForTimeout() {
		srvLog.Error("Shutdown timeout")
	}
}

func monOnConfigChange(cfg *ini.IniCfg, changeCount int) {
	monCfgLock.Lock()
	defer monCfgLock.Unlock()

	srvLog.Info("monOnConfigChange")

	section := cfg.GetSection("net")

	val := section.GetFirstVal("MonitorSrvPort")
	monPort := val.GetValInt(0, monDefaultSrvPort)

	monUri = fmt.Sprintf(
		"ws://%s:%d/register?key=%s",
		monDefaultSrvAddr,
		monPort,
		monKeyStr,
	)

	srvLog.Info("MonitorSrvAddr: %s", monUri)
}

func monOnStatusCmdUri(resp http.ResponseWriter, req *http.Request) {
	defer crash.HandleAll()

	if req.Method != "POST" {
		errMsg := fmt.Sprintf("Unsupported method: %s", req.Method)
		http.Error(resp, errMsg, http.StatusBadRequest)
		Log().Error(errMsg)
		return
	}

	monSendStatus()
}

// monAsyncWork handles connect/reconnect to monitor server, shutdown, and
// periodic pings to the server
func monAsyncWork() {
	defer _monShutdown.Complete()
	monCallHome()

	shutdown := false

	for {
		select {
		case <-_monShutdown.Signal:
			if shutdown {
				return
			}

			shutdown = true
			close(monDisconnectChan)
		case <-monDisconnectChan:
			if shutdown {
				return
			}

			<-time.After(5 * time.Second)
			monCallHome()
		case <-time.After(1 * time.Minute):
			if shutdown {
				return
			}
			monSendCounters()
		}
	}
}

func monSendAppInfo() {
	data, err := getAppInfoJSON()
	if err != nil {
		Log().Error("Error marshaling app info: %v", err)
		return
	}

	monSend(monGetMsg(MonMsgAppInfo, data))
}

func monSendCounters() {
	data, err := getCountersJSON()
	if err != nil {
		Log().Error("Error marshaling counter info: %v", err)
		return
	}

	monSend(monGetMsg(MonMsgCounters, data))
}

func monSendIpsBuffer() {
	data, err := ipsGetBufferJSON()
	if err != nil {
		Log().Error("Error marshaling ips info: %v", err)
		return
	}

	monSend(monGetMsg(MonMsgIpsStats, data))
}

func monSendLogUpdate(log *log.LogMsg) {
	data, err := json.Marshal(log)
	if err != nil {
		Log().ErrorLocal("Error marshaling log info: %v", err)
		return
	}

	monSend(monGetMsg(MonMsgOnLog, data))
}

func monSendIpsUpdate(req *RequestLog) {
	data, err := json.Marshal(req)
	if err != nil {
		Log().Error("Error marshaling ips update info: %v", err)
		return
	}

	monSend(monGetMsg(MonMsgOnIpsReq, data))
}

func monSendStatus() {
	monSendAppInfo()
	monSendCounters()
	monSendIpsBuffer()
}

func monGetMsg(msgType byte, data []byte) *MonUpdateMsg {
	return &MonUpdateMsg{
		ServiceName: app.GetName(),
		MsgType:     msgType,
		NetId:       netGetNetId(),
		Data:        data,
	}
}

func monHandleCommand(command string) {
	Log().Info("Websocket command received: %s", command)

	switch command {
	case "status":
		monSendStatus()
	case "appinfo":
		monSendAppInfo()
	case "counters":
		monSendCounters()
	case "ipstats":
		monSendIpsBuffer()
	default:
		Log().Error("Received invalid command from server: %s", command)
	}
}

func monCallHome() {
	go func() {
		defer crash.HandleAll()

		monCfgLock.RLock()
		uri := monUri
		monCfgLock.RUnlock()

		c, _, err := websocket.DefaultDialer.Dial(uri, nil)
		if err != nil {
			Log().Error("Error connecting to monitoring service: %v", err)
			if !_monShutdown.IsShutdown() {
				monDisconnectChan <- 0
			}
			return
		}

		monConn = c
		c.SetCloseHandler(monOnClose)

		Log().Info("Connection successful")

		monSendStatus()
		monHandleConn()
	}()
}

func monOnClose(t int, msg string) error {
	defer crash.HandleAll()

	Log().Info("Received Close message from server: %s (type %d)", t, msg)
	monConn.Close()

	return nil
}

func monHandleConn() {
	defer monConn.Close()

	for {
		mt, msg, err := monConn.ReadMessage()
		if err != nil {
			Log().Error("Websocket Receive error: %v", err)
			go func() {
				defer crash.HandleAll()
				if !_monShutdown.IsShutdown() {
					monDisconnectChan <- 0
				}
			}()
			return
		}

		switch mt {
		case websocket.TextMessage:
			go func() {
				defer crash.HandleAll()
				monHandleCommand(string(msg))
			}()
		default:
			Log().Info("Unsupported message type received: %d", mt)
		}
	}
}
