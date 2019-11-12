//  ---------------------------------------------------------------------------
//
//  all_test.go
//
//  Copyright (c) 2015, Jared Chavez.
//  All rights reserved.
//
//  Use of this source code is governed by a BSD-style
//  license that can be found in the LICENSE file.
//
//  -----------

package srvApp

import (
	"testing"
	"time"
)

func TestSrvInitAndShutdown(t *testing.T) {
	Init()

	if !testSrvInit() {
		t.Errorf("Error initializing test srv")
	}

	ShutdownNotify(testSrvShutdown)

	resultChan := make(chan int, 1)

	go func() {
		resultCode := Run()
		resultChan <- resultCode
	}()

	go func() {
		<-time.After(2 * time.Second)
		SignalShutdown(0)
	}()

	select {
	case resultCode := <-resultChan:
		if resultCode != 0 {
			t.Errorf("Server run result code != 0 (got %d)", resultCode)
		}
	case <-time.After(4 * time.Second):
		t.Errorf("Timeout during test")
	}
}

func testSrvInit() bool {
	if QueryShutdown() {
		return false
	}

	srvLog.Info("testSrvInit")
	return true
}

func testSrvShutdown() {
	srvLog.Info("testSrvShutdown")
}
