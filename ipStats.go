package srvApp

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/xaevman/crash"
	"github.com/xaevman/ini"
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
	ipsLock         sync.RWMutex
	ipsShutdownChan = make(chan bool, 0)
	ipsStats        = make(map[string]*IPStatus)
)

// default config vaules
const (
	ipsDefaultReaperFreqMin = 30
	ipsDefaultMaxReqAgeHrs  = 24
)

// ips config synchronization
var (
	ipsCfgLock       sync.RWMutex
	ipsReaperFreqMin float64
	ipsMaxReqAgeHrs  float64
)

type IPStatus struct {
	Count    uint64
	ErrCount uint64
	Host     string
	Requests []*ReqStatus
	StatLock sync.Mutex `json:"-"`
}

func (this *IPStatus) deleteReqAtIndex(i int) {
	this.Requests[i] = this.Requests[len(this.Requests)-1]
	this.Requests[len(this.Requests)-1] = nil
	this.Requests = this.Requests[:len(this.Requests)-1]
}

type ReqStatus struct {
	AccessLevel int
	Path        string
	Pattern     string
	Status      int
	Timestamp   time.Time
}

func ipsInit() {
	ini.Subscribe(appConfig, ipsOnConfigChange)
	go ipsRunReaper()

	httpSrv.RegisterHandler(
		"/debug/ipstats/",
		ipsOnUri,
		PRIVATE_HANDLER,
		ACCESS_LEVEL_ADMIN,
	)
}

func ipsShutdown() {
	ipsShutdownChan <- true
}

func ipsOnConfigChange(cfg *ini.IniCfg, changeCount int) {
	ipsCfgLock.Lock()
	defer ipsCfgLock.Unlock()

	srvLog.Info("ipsOnConfigChange")

	section := cfg.GetSection("net")

	val := section.GetFirstVal("IpStatsReaperFreqMin")
	ipsReaperFreqMin = val.GetValFloat64(0, ipsDefaultReaperFreqMin)
	srvLog.Info("IpStatsReaperFreqMin: %.2f", ipsReaperFreqMin)

	val = section.GetFirstVal("IPStatsMaxRequestAgeHrs")
	ipsMaxReqAgeHrs = val.GetValFloat64(0, ipsDefaultMaxReqAgeHrs)
	srvLog.Info("IPStatsMaxRequestAgeHrs: %.2f", ipsMaxReqAgeHrs)
}

func ipsRunReaper() {
	for {
		freqMin := time.Duration(ipsGetReaperFreqMin())

		select {
		case <-ipsShutdownChan:
			return
		case <-time.After(freqMin * time.Minute):
			ipsPurgeOldStats()
		}
	}
}

func ipsGetReaperFreqMin() float64 {
	ipsCfgLock.RLock()
	defer ipsCfgLock.RUnlock()

	return ipsReaperFreqMin
}

func ipsGetMaxReqAgeHrs() float64 {
	ipsCfgLock.RLock()
	defer ipsCfgLock.RUnlock()

	return ipsMaxReqAgeHrs
}

func ipsOnUri(resp http.ResponseWriter, req *http.Request) {
	defer crash.HandleAll()

	ipsLock.RLock()
	defer ipsLock.RUnlock()

	body, err := json.MarshalIndent(ipsStats, "", "    ")
	if err != nil {
		http.Error(resp, "StatusInternalServerError", http.StatusInternalServerError)
		srvLog.Error("ipsOnUri error: %v", err)
		return
	}

	resp.Write(body)
}

func ipsPurgeOldStats() {
	ipsLock.Lock()
	defer ipsLock.Unlock()

	maxReqAgeHrs := ipsGetMaxReqAgeHrs()
	purgeCount := 0

	for k, _ := range ipsStats {
		ipsStats[k].StatLock.Lock()
		defer ipsStats[k].StatLock.Unlock()

		for i := range ipsStats[k].Requests {
			dt := time.Since(ipsStats[k].Requests[i].Timestamp)
			if dt.Hours() > maxReqAgeHrs {
				ipsStats[k].deleteReqAtIndex(i)
				purgeCount++
			}
		}
	}

	srvLog.Info("Purged %d old IPStat entries", purgeCount)
}

func ipsLogStats(host string, uri, pattern string, access, status int) {
	var hostStats *IPStatus
	var ok bool

	ipsLock.RLock()
	hostStats, ok = ipsStats[host]
	ipsLock.RUnlock()

	if !ok {
		tmp := &IPStatus{
			Count:    0,
			ErrCount: 0,
			Host:     host,
			Requests: make([]*ReqStatus, 0, 10),
		}

		ipsLock.Lock()
		hostStats, ok = ipsStats[host]
		if !ok {
			ipsStats[host] = tmp
			hostStats = tmp
		}
		ipsLock.Unlock()
	}

	hostStats.StatLock.Lock()
	defer hostStats.StatLock.Unlock()

	hostStats.Count++
	if status > 0 {
		hostStats.ErrCount++
	}

	reqStats := &ReqStatus{
		AccessLevel: access,
		Path:        uri,
		Pattern:     pattern,
		Status:      status,
		Timestamp:   time.Now(),
	}

	hostStats.Requests = append(hostStats.Requests, reqStats)
}
