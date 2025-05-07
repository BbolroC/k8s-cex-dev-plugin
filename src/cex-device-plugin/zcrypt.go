/*
 * Copyright 2021 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Author(s): Harald Freudenberger <freude@de.ibm.com>
 *
 * s390 zcrypt kubernetes device plugin
 * zcrypt multiple device nodes handling functions
 */

package main

import (
	"log"
	"os"
)

const (
	zcryptclassdir     = "/sys/class/zcrypt"
	zcryptvdevdir      = "/sys/devices/virtual/zcrypt"
	zcryptnodefilemode = 0666
)

func zcryptHasNodesSupport() bool {
	_, err := os.Stat(zcryptclassdir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Zcrypt: No zcrypt multiple nodes support ('%s' does not exist)\n", zcryptclassdir)
			return false
		} else {
			log.Printf("Zcrypt: Error reading zcrypt multiple nodes support dir: %s\n", err)
			return false
		}
	}
	return true
}

func zcryptNodeExists(nodename string) bool {
	dirname := zcryptvdevdir + "/" + nodename
	_, err := os.Stat(dirname)
	if err != nil {
		return false
	}
	return true
}

func zcryptDestroyNode(nodename string) error {
	// destroy the zcrypt device node via writing to /sys/class/zcrypt/destroy
	destroyfname := zcryptclassdir + "/" + "destroy"
	f, err := os.OpenFile(destroyfname, os.O_WRONLY, 0)
	if err != nil {
		log.Printf("Zcrypt: Can't open file '%s': %s\n", destroyfname, err)
		return err
	}
	defer f.Close()
	_, err = f.WriteString(nodename)
	if err != nil {
		log.Printf("Zcrypt: Error writing to '%s': %s\n", destroyfname, err)
		return err
	}
	return nil
}
