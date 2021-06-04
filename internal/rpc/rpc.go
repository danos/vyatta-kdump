// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package rpc

type RPCInput struct {
	Index []int32 `rfc7951:"vyatta-system-crash-dump-v1:index"`
}
type CrashData struct {
	Index    int32  `rfc7951:"index"`
	FileName string `rfc7951:"filename"`
	DMesg    string `rfc7951:"dmesg"`
}

type CrashDMesgOut struct {
	CrashInfo []CrashData `rfc7951:"vyatta-system-crash-dump-v1:crash-info"`
}
