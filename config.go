// Copyright (c) 2021, AT&T Intellectual Property. All rights reserved.
// SPDX-License-Identifier: GPL-2.0-only
package vci_kdump

import (
	"errors"
	"fmt"
	"github.com/danos/encoding/rfc7951"
	"github.com/danos/vci"
	cfg "github.com/danos/vyatta-kdump/internal/config"
	"github.com/danos/vyatta-kdump/internal/kdump"
	"github.com/danos/vyatta-kdump/internal/log"
	"io/ioutil"
	"strings"
	"sync"
	"sync/atomic"
)

type Config struct {
	writeMu       sync.Mutex
	currentConfig atomic.Value
	//options
	cacheFile string
	client    *vci.Client
}

var instance_cfg *Config

func ConfigNew(client *vci.Client, cache string) *Config {
	cfg := &Config{}
	cfg.currentConfig.Store(&ConfigData{})

	cfg.client = client
	cfg.cacheFile = cache

	cfg.readCache()
	instance_cfg = cfg
	return cfg
}

type ConfigData struct {
	System struct {
		KDump *cfg.KDumpData `rfc7951:"vyatta-system-crash-dump-v1:kernel-crash-dump,omitempty"`
	} `rfc7951:"vyatta-system-v1:system"`
}

func (c *Config) Get() *ConfigData {
	return c.currentConfig.Load().(*ConfigData)
}

func (c *Config) Set(newConfig *ConfigData) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.writeCache(newConfig)

	if newConfig != nil {
		c.applyConfig(newConfig)
		c.currentConfig.Store(newConfig)
	}
	m := setResult(newConfig)
	log.Ilog.Println(m)
	return errors.New(m)
}

func (c *Config) Check(proposedConfig *ConfigData) error {
	return nil
}

func (c *Config) applyConfig(cfg *ConfigData) error {
	kd := cfg.System.KDump
	if kd != nil && kd.Enable {
		m, err := kd.ReservedMemStr()
		if err == nil {
			err = kdump.ReserveMem(m)
		}
		if err != nil {
			return errors.New(fmt.Sprintf("Failed setup grub to reserve memory next boot: %s", err))
		}
	} else {
		err := kdump.ReserveMem("0") // Free up reserved memory, on error log and continue.
		if err != nil {
			log.Wlog.Println("Failed to release reserved memory: %s", err)
		}
	}
	if kd != nil && kd.IsEnabled() {
		err := kdump.Enable(kd.FilesToSave, kd.DeleteOldFiles)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to Enable kernel crash dump: %s", err))
		}
		log.Ilog.Printf("Kdump enabled: Enable:%t FilesToSave: %s, DeleteOldFile %t ReservedMemory %v",
			kd.Enable, kd.FilesToSave, kd.DeleteOldFiles, kd.ReservedMemory)
	} else {
		kdump.Disable(!kd.Enable)
		log.Ilog.Println("Kdump Disabled")
	}
	return nil
}

func setResult(newcfg *ConfigData) string {
	if newcfg == nil {
		return "Error: No kernel crash dump configuration."
	}

	kd := newcfg.System.KDump

	ready := kdump.GetKDumpState() == kdump.KDumpReady
	reboot := kdump.IsRebootNeeded()
	reserved := kdump.CrashKernelMemory != 0

	// Memory Reservation Check
	mem := ""
	if reboot && reserved && kd.Enable {
		mem = "Reserved Memory changes will take effect on next boot."
	} else if reboot && reserved && !kd.Enable {
		mem = "Reseved memory will be released on next boot."
	} else if reboot && !reserved && kd.Enable {
		mem = "Memory will be reserved on next boot."
	}

	// Enabled or disabled
	state := ""
	if kd.IsEnabled() {
		if ready {
			state = "Kernel crash dump is enabled."
		} else if reboot {
			state = "kernel crash dump will be enabled on next-boot."
		} else {
			state = "ERROR: kernel crash dump is not enabled."
		}
	} else if !ready {
		state = "kernel crash dump is disabled."
	} else {
		state = "Error: Failed to disable kernel crash dump capture."
	}

	return strings.Join([]string{state, mem}, " ")
}

func (c *Config) readCache() {
	cache := &ConfigData{}
	defer func() {
		c.currentConfig.Store(cache)
	}()
	if c.cacheFile == "" {
		return
	}
	buf, err := ioutil.ReadFile(c.cacheFile)
	if err != nil {
		log.Wlog.Println("read-cache:", err)
		return
	}
	err = rfc7951.Unmarshal(buf, cache)
	if err != nil {
		log.Wlog.Println("read-cache:", err)
		return
	}
	err = c.Set(cache)
	if err != nil {
		log.Elog.Println("read-cache:", err)
	}
}

func (c *Config) writeCache(new *ConfigData) {
	c.currentConfig.Store(new)
	if c.cacheFile == "" {
		return
	}
	buf, err := rfc7951.Marshal(new)
	if err != nil {
		log.Elog.Println("write-cache:", err)
		return
	}
	err = ioutil.WriteFile(c.cacheFile, buf, 0600)
	if err != nil {
		log.Elog.Println("write-cache:", err)
	}
}
