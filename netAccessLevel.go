package srvApp

import (
    "net"
    "strings"
)

const (
    ACCESS_LEVEL_NONE = iota
    ACCESS_LEVEL_USER
    ACCESS_LEVEL_ADMIN
)

var AccessLevelStr = map[int]string {
    ACCESS_LEVEL_NONE  : "ACCESS_LEVEL_NONE",
    ACCESS_LEVEL_USER  : "ACCESS_LEVEL_USER",
    ACCESS_LEVEL_ADMIN : "ACCESS_LEVEL_ADMIN",
}

type AccessNet struct {
    Level  int
    Subnet *net.IPNet
}

var netAccessList []*AccessNet


// netAccessList maps CIDR networks to access levels for IP-based
// security of request handlers.
func GetAccessLevel (host string) int {
    netCfgLock.RLock()
    defer netCfgLock.RUnlock()

    return getAccessLevel(host)
}

func getAccessLevel (host string) int {
    ip := net.ParseIP(host)

    for i := range netAccessList {
        if netAccessList[i].Subnet.Contains(ip) {
            return netAccessList[i].Level
        }
    }

    return ACCESS_LEVEL_NONE
}

func ParseAccessLevel(lvlStr string) int {
    netCfgLock.RLock()
    defer netCfgLock.RUnlock()

    return parseAccessLevel(lvlStr)
}

func parseAccessLevel(lvlStr string) int {
    switch (strings.ToLower(lvlStr)) {
    case "admin":
        return ACCESS_LEVEL_ADMIN
    case "user":
        return ACCESS_LEVEL_USER
    case "none":
        return ACCESS_LEVEL_NONE
    default:
        return ACCESS_LEVEL_NONE
    }
}
