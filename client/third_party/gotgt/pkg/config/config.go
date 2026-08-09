/*
Copyright 2016 The GoStor Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package config contains how to get/save config parameters from file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

/*
Format of configuration file

{
    "storages": [
        {
            "deviceID": integer, uniqu device id,
            "path": string, <protocol>:<absolute/file/path>",
            "online": bool, online/offline
        }
    ],

	"portals": [
    	{
    		"id": integer, uniqu portal id
    		"portal":string, <IP>:<PORT>
    	}
    ],

    "targets": {
        <target name >: {
            "tpgts":{
            	<tpgt number>: [<portal id[,portal id....]]
            }
            //n shoud be an value from 0 ~ 65535

            "luns": {
                <lu number for the target>: <mappingd with the device ID>
            }
        }
    }
}

Example of the configuration file

{
    "storages": [
        {
            "deviceID": 1000,
            "path": "file:/tmp/disk.img",
            "online": true
        }
    ],
    "iscsiportals":[
        {
            "id":0,
            "portal":"192.168.159.1:3260"
        }
    ],
    "iscsitargets": {
        "iqn.2016-09.com.gotgt.gostor:example_tgt_0": {
            "tpgts": {
                "1":[0]
            }
            ,
            "luns": {
                "1": 1000
            }
        }
    }
}

*/

const (
	// ConfigFileName is the name of config file
	ConfigFileName = "config.json"
)

var (
	configDir = os.Getenv("GOSTOR_CONFIG")
	config    *Config
)

type BackendStorage struct {
	DeviceID         uint64 `json:"deviceID"`
	Path             string `json:"path"`
	Online           bool   `json:"online"`
	ThinProvisioning bool   `json:"thinProvisioning"`
	BlockShift       uint   `json:"blockShift"`
}

type ISCSIPortalInfo struct {
	ID     uint16 `json:"id"`
	Portal string `json:"portal"`
}

type ISCSITarget struct {
	TPGTs map[string][]uint64 `json:"tpgts"`
	LUNs  map[string]uint64   `json:"luns"`
}

type Config struct {
	Storages     []BackendStorage       `json:"storages"`
	ISCSIPortals []ISCSIPortalInfo      `json:"iscsiportals"`
	ISCSITargets map[string]ISCSITarget `json:"iscsitargets"`
}

func init() {
	if configDir == "" {
		homeDir, _ := homedir.Dir()
		configDir = filepath.Join(homeDir, ".gotgt")
	}
}

// ConfigDir returns the directory the configuration file is stored in
func ConfigDir() string {
	return configDir

}
func GetConfig() *Config {
	return config
}

// Load reads the configuration files in the given directory and return values.
func Load(configDir string) (*Config, error) {
	if configDir == "" {
		configDir = ConfigDir()
	}

	filename := filepath.Join(configDir, ConfigFileName)
	config = &Config{
		ISCSITargets: make(map[string]ISCSITarget),
	}

	// Try happy path first - latest config file
	if _, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		// if file is there but we can't stat it for any reason other
		// than it doesn't exist then stop
		return config, fmt.Errorf("%s - %v", filename, err)
	}
	file, err := os.Open(filename)
	if err != nil {
		return config, fmt.Errorf("%s - %v", filename, err)
	}
	defer file.Close()
	if err = json.NewDecoder(file).Decode(config); err != nil {
		return config, err
	}
	if err != nil {
		err = fmt.Errorf("%s - %v", filename, err)
	}
	return config, err
}

// Save encodes and writes out all the authorization information
func (config *Config) Save(filename string) error {
	if filename == "" {
		return fmt.Errorf("Can't save config with empty filename")
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
