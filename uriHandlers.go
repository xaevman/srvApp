package srvApp

import (
    "encoding/json"
    "net/http"
)

func OnPrivHandlerUri(resp http.ResponseWriter, req *http.Request) {
    handlers  := Http.GetHandlerKeys()
    data, err := json.MarshalIndent(&handlers, "", "    ")
    if err != nil {
        resp.WriteHeader(http.StatusInternalServerError)
        return
    }

    resp.Write(data)
}
