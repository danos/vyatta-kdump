// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package vci_kdump

import (
	"fmt"
	"github.com/danos/vyatta-kdump/internal/kdump"
	rpc "github.com/danos/vyatta-kdump/internal/rpc"
)

type RPC struct {
	conf *Config
}

func RPCNew(conf *Config) *RPC {
	return &RPC{
		conf: conf,
	}
}

func (r *RPC) DeleteCrashDumps(in rpc.RPCInput) (struct{}, error) {
	err := kdump.DelCrashDumps(in.Index)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, nil
}

func (r *RPC) GetCrashDmesg(in rpc.RPCInput) (*rpc.CrashDMesgOut, error) {
	crash_dir, crashdumps := kdump.GetCrashFiles()

	res := &rpc.CrashDMesgOut{}
	if len(in.Index) != 0 {
		res.CrashInfo = make([]rpc.CrashData, len(in.Index))
		for i, index := range in.Index {
			res.CrashInfo[i].Index = index
		}
	} else {
		res.CrashInfo = make([]rpc.CrashData, len(crashdumps))
		for i := 0; i < len(crashdumps); i++ {
			res.CrashInfo[i].Index = int32(i)
		}
	}

	for i := 0; i < len(res.CrashInfo); i++ {
		index := res.CrashInfo[i].Index
		if int(index) >= len(crashdumps) || int(-index) > len(crashdumps) {
			continue
		}
		res.CrashInfo[i].FileName = fmt.Sprintf("%s/%s", crash_dir, crashdumps[index].Name())
		res.CrashInfo[i].DMesg = kdump.GetCrashDMsg(crashdumps[index])
	}
	return res, nil
}
