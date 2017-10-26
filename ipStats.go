package srvApp

import (
	"container/ring"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/xaevman/crash"
	"github.com/xaevman/shutdown"
)

// REQ_STATUS enumeration
const (
	REQ_STATUS_SUCCESS = iota
	REQ_STATUS_NOT_FOUND
	REQ_STATUS_IP_DENY
	REQ_STATUS_GEO_DENY
	REQ_STATUS_THROTTLE_DENY
)

// module data
var (
	ipsLock      sync.RWMutex
	ipsBuffer    = ring.New(1000)
	ipsEvents    = make(chan *RequestLog, 0)
	_ipsShutdown = shutdown.New()
)

type RequestLog struct {
	Host        string
	Path        string
	Pattern     string
	Status      int
	AccessLevel int
	Timestamp   time.Time
	GeoData     *GeoIpData
}

func ipsInit() {
	srvLog.Info("ips Init")

	httpSrv.RegisterHandler(
		"/debug/ipstats/",
		ipsOnUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)

	go func() {
		defer crash.HandleAll()
		defer _ipsShutdown.Complete()

		for {
			select {
			case <-_ipsShutdown.Signal:
				return
			case data, more := <-ipsEvents:
				if !more {
					return
				}

				data.GeoData = geoResolveAddr(data.Host)

				func() {
					ipsLock.Lock()
					defer ipsLock.Unlock()

					ipsBuffer.Value = data
					ipsBuffer = ipsBuffer.Next()
				}()

				monSendIpsUpdate(data)
			}
		}
	}()
}

func ipsOnUri(resp http.ResponseWriter, req *http.Request) {
	defer crash.HandleAll()

	js, err := ipsGetBufferJSON()
	if err != nil {
		srvLog.Error("Error parsing buffer: %v", err)
		http.Error(resp, "Error parsing buffer", http.StatusInternalServerError)
		return
	}

	resp.Write(js)
}

func ipsShutdown() {
	srvLog.Info("ips Shutdown")
	_ipsShutdown.Start()
	if _ipsShutdown.WaitForTimeout() {
		srvLog.Error("Shutdown timeout")
	}
}

func ipsGetBuffer() []*RequestLog {
	tmp := make([]*RequestLog, 0, ipsBuffer.Len())

	func() {
		ipsLock.RLock()
		defer ipsLock.RUnlock()

		ipsBuffer.Do(func(item interface{}) {
			if item == nil {
				return
			}

			tmp = append(tmp, item.(*RequestLog))
		})
	}()

	return tmp
}

func ipsGetBufferJSON() ([]byte, error) {
	tmp := ipsGetBuffer()
	return json.Marshal(tmp)
}

func ipsLogStats(host string, uri, pattern string, access, status int) {
	newLog := &RequestLog{
		Host:        host,
		Path:        uri,
		Pattern:     pattern,
		Status:      status,
		AccessLevel: access,
		Timestamp:   time.Now(),
	}

	go func() {
		defer crash.HandleAll()

		select {
		case <-_ipsShutdown.Signal:
		case ipsEvents <- newLog:
		case <-time.After(2 * time.Second):
			srvLog.Error("Timeout queueing ips stats data")
		}
	}()
}
