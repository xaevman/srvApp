package srvApp

import (
	gjson "encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/xaevman/ini"
)

const GEO_DATA_FILE = "geo.dat"

type GeoIpData struct {
	StartIPNum  uint64  `json:"-"`
	EndIPNum    uint64  `json:"-"`
	CountryCode string  `json:"country_code,omitempty"`
	Country     string  `json:"country_name,omitempty"`
	Region      string  `json:"region_name,omitempty"`
	City        string  `json:"city,omitempty"`
	Lat         float32 `json:"latitude,omitempty"`
	Long        float32 `json:"longitude,omitempty"`
	ZipCode     string  `json:"zip_code,omitempty"`
}

var (
	geoDataLocal = &GeoIpData{
		Country:     "RFC1918",
		CountryCode: "RF",
		Region:      "RFC1918",
		City:        "RFC1918",
		ZipCode:     "RFC1918",
	}

	geoDataUnknown = &GeoIpData{
		Country:     "Unknown",
		CountryCode: "UU",
		Region:      "Unknown",
		City:        "Unknown",
		ZipCode:     "Unknown",
	}
)

// geo module config synchronization
var (
	geoCfgLock    sync.RWMutex
	geoSecEnabled bool
	geoServiceUri string
)

var (
	geoDataMap      = make(map[string]*GeoIpData)
	geoCountryPerms map[string]bool
	geoLastUpdate   = make(map[string]time.Time)
)

func geoInit() {
	defer ini.Subscribe(appConfig, geoOnConfigChange)
}

func geoShutdown() {

}

func geoOnConfigChange(cfg *ini.IniCfg, changeCount int) {
	geoCfgLock.Lock()
	defer geoCfgLock.Unlock()

	srvLog.Info("geoOnConfigChange")

	geoCountryPerms = make(map[string]bool)

	section := cfg.GetSection("net")

	val := section.GetFirstVal("GeoIPSecurityEnabled")
	geoSecEnabled = val.GetValBool(0, true)
	srvLog.Info("GeoIPSecurityEnabled: %t", geoSecEnabled)

	val = section.GetFirstVal("GeoIPServiceUri")
	geoServiceUri = val.GetValStr(0, "")
	srvLog.Info("GeoIPServiceUri: %s", geoServiceUri)

	vals := section.GetVals("GeoIPAllowedCountry")
	for i := range vals {
		country := vals[i].GetValStr(0, "")
		if country != "" {
			geoCountryPerms[country] = true
			srvLog.Info("GeoIPAllowedCountry: %s", country)
		}
	}
}

func geoGetServiceUri() string {
	geoCfgLock.RLock()
	defer geoCfgLock.RUnlock()

	return geoServiceUri
}

func geoGetEnabled() bool {
	geoCfgLock.RLock()
	defer geoCfgLock.RUnlock()

	return geoSecEnabled
}

func geoResolveAddr(host string) *GeoIpData {
	// if it's an RFC1918 network there's no need
	// to do anything fancy
	if httpSrv.IsPrivateNetwork(host) {
		return geoDataLocal
	}

	// see if we have the answer cached already
	geoCfgLock.RLock()
	geoData, ok := geoDataMap[host]
	geoCfgLock.RUnlock()
	if ok {
		lastUpdate, ok := geoLastUpdate[host]
		if ok && lastUpdate.Before(time.Now().Add(-(time.Hour * 24))) {
			// cached entry is more than 24 hrs old - throw it away
			delete(geoLastUpdate, host)
		} else {
			return geoData
		}
	}

	// nope, let's request it
	uri := fmt.Sprintf("%s/%s", geoGetServiceUri(), host)
	resp, err := http.Get(uri)
	if err != nil {
		return geoDataUnknown
	}
	defer resp.Body.Close()

	rawData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return geoDataUnknown
	}

	var newGeo GeoIpData

	err = gjson.Unmarshal(rawData, &newGeo)
	if err != nil {
		return geoDataUnknown
	}

	geoCfgLock.Lock()
	geoDataMap[host] = &newGeo
	geoLastUpdate[host] = time.Now()
	geoCfgLock.Unlock()

	return &newGeo
}

func geoAuthorizeHost(host string) (string, bool) {
	if !geoGetEnabled() {
		return "", true
	}

	geoData := geoResolveAddr(host)

	geoCfgLock.RLock()
	allowed, ok := geoCountryPerms[geoData.Country]
	geoCfgLock.RUnlock()

	if !ok {
		allowed = false
	}

	return geoData.Country, allowed
}
