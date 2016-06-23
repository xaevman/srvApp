package srvApp

import (
	"bytes"
	"compress/zlib"
	gjson "encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/xaevman/ini"
	"github.com/xaevman/json"
)

const GEO_DATA_FILE = "geo.dat"

// geo module config synchronization
var (
	geoCfgLock    sync.RWMutex
	geoSecEnabled bool
	geoServiceUri string
)

var (
	geoCountryMap   map[string]string
	geoCountryPerms map[string]bool
)

func geoInit() {
	defer ini.Subscribe(appConfig, geoOnConfigChange)

	geoCfgLock.Lock()
	defer geoCfgLock.Unlock()

	f, err := os.Open(GEO_DATA_FILE)
	if err != nil {
		geoCountryMap = make(map[string]string)
		srvLog.Error("geoInit error: %v", err)
		return
	}
	defer f.Close()

	var buffer bytes.Buffer

	cmpReader, err := zlib.NewReader(f)
	if err != nil {
		geoCountryMap = make(map[string]string)
		srvLog.Error("geoInit error: %v", err)
		return
	}
	defer cmpReader.Close()

	buffer.ReadFrom(cmpReader)

	err = gjson.Unmarshal(buffer.Bytes(), &geoCountryMap)
	if err != nil {
		geoCountryMap = make(map[string]string)
		srvLog.Error("geoInit error: %v", err)
		return
	}

	srvLog.Info(
		"GeoIP data loaded from disk (%d entries)",
		len(geoCountryMap),
	)
}

func geoShutdown() {
	geoCfgLock.RLock()
	defer geoCfgLock.RUnlock()

	srvLog.Info(
		"Saving geo ip tables (%d entries)",
		len(geoCountryMap),
	)

	f, err := os.Create(GEO_DATA_FILE)
	if err != nil {
		srvLog.Error("geoShutdown error: %v", err)
		return
	}
	defer f.Close()

	data, err := gjson.Marshal(geoCountryMap)
	if err != nil {
		srvLog.Error("geoShutdown error: %v", err)
		return
	}

	bReader := bytes.NewReader(data)
	cmpWriter := zlib.NewWriter(f)
	defer cmpWriter.Close()

	io.Copy(cmpWriter, bReader)
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

func geoAuthorizeHost(host string) (string, bool) {
	if !geoGetEnabled() {
		return "", true
	}

	var allowed bool
	var country string
	var ok bool

	geoCfgLock.RLock()
	country, ok = geoCountryMap[host]
	geoCfgLock.RUnlock()

	if !ok {
		tmp := geoQueryCountry(host)

		geoCfgLock.Lock()
		country, ok = geoCountryMap[host]
		if !ok {
			geoCountryMap[host] = tmp
			country = tmp

		}
		geoCfgLock.Unlock()
	}

	geoCfgLock.RLock()
	allowed, ok = geoCountryPerms[country]
	geoCfgLock.RUnlock()

	if !ok {
		geoCfgLock.Lock()
		allowed, ok = geoCountryPerms[country]
		if !ok {
			geoCountryPerms[country] = false
			allowed = false
		}
		geoCfgLock.Unlock()

		return country, false
	}

	return country, allowed
}

func geoQueryCountry(host string) string {
	if httpSrv.IsPrivateNetwork(host) {
		return "local"
	}

	svcUri := geoGetServiceUri()
	geoRequest := fmt.Sprintf("%s/%s", svcUri, host)
	resp, err := http.Get(geoRequest)

	if err != nil {
		srvLog.Error("Geo IP country lookup failure: %s (%v)", host, err)
		return "serviceQueryFailure"
	}

	defer resp.Body.Close()

	var buffer bytes.Buffer
	buffer.ReadFrom(resp.Body)

	j := json.Parse(buffer.Bytes())
	country := json.Search(j, "country_code").Value().(string)

	if country == "" {
		country = "serviceQueryFailure::empty"
	}

	return country
}
