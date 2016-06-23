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
)

func TestSrv(t *testing.T) {
	if !StartSingleton() {
		t.Failed()
	}

	if !Shutdown() {
		t.Failed()
	}
}
