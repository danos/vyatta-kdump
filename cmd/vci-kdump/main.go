// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"log"

	"github.com/danos/vci"
	"github.com/danos/vyatta-kdump"
)

const (
	cfgFile = "/var/run/kdump.cfg"
)

func main() {
	comp := vci.NewComponent("net.vyatta.vci.kdump")
	conf := vci_kdump.ConfigNew(comp.Client(), cfgFile)
	state := vci_kdump.StateNew(conf)
	rpc := vci_kdump.RPCNew(conf)
	comp.Model("net.vyatta.vci.kdump.v1").
		Config(conf).
		State(state).
		RPC("vyatta-system-crash-dump-v1", rpc)
	err := comp.Run()
	if err != nil {
		log.Fatal(err)
	}
	comp.Wait()
}
