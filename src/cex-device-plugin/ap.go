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
 * AP bus related functions
 */

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
)

const (
	apsysfsdir     = "/sys/bus/ap"
	apsysfsdevsdir = "/sys/devices/ap"
	// Estimate how much space an APQN requires when printing
	apqnstringestimate = 6 + 3 + 3 + 4 + 5 + 1
)

type APQN struct {
	Adapter int    `json:"adapter"`
	Domain  int    `json:"domain"`
	Gen     string `json:"gen"`    // something like "cex7"
	Mode    string `json:"mode"`   // mode string "ep11" or "cca" or "accel"
	Online  bool   `json:"online"` // true = online, false = offline
}

func (a *APQN) String() string {
	return fmt.Sprintf("(%d,%d,%s,%s,%v)", a.Adapter, a.Domain, a.Gen, a.Mode, a.Online)
}

type APQNList []*APQN

func (l APQNList) String() string {
	var b strings.Builder
	b.Grow(len(l) * (apqnstringestimate + 2))
	for i, e := range l {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s", e)
	}
	return b.String()
}

func apHasApSupport() bool {

	_, err := os.Stat(apsysfsdir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Ap: No AP bus support (AP bus sysfs dir does not exist)\n")
		} else {
			log.Printf("Ap: Error reading AP bus sysfs dir: %s\n", err)
		}
		return false
	}

	return true
}

func apEqualAPQNLists(l1, l2 APQNList) bool {

	var found bool

	if len(l1) != len(l2) {
		return false
	}

	for _, a1 := range l1 {
		found = false
		for _, a2 := range l2 {
			if a1.Adapter == a2.Adapter && a1.Domain == a2.Domain {
				if a1.Gen != a2.Gen {
					return false
				}
				if a1.Mode != a2.Mode {
					return false
				}
				if a1.Online != a2.Online {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
