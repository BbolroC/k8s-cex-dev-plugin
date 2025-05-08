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
 * shadow ap sysfs functions
 */

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
)

var shadowbasedir = getenvstring("SHADOWSYSFS_BASEDIR", "/var/tmp/shadowsysfs", dirExistsWritable)

func getenvstring(envvar, defaultval string, check func(string, string)) string {
	val, isset := os.LookupEnv(envvar)
	if !isset {
		val = defaultval
	}
	check(val, envvar)
	return val
}

func dirExistsWritable(d, envvar string) {
	fi, err := os.Stat(d)
	if err != nil {
		log.Fatalf("Shadowsysfs: Invalid %s setting: %s: %v", envvar, d, err)
	}
	if !fi.IsDir() || (fi.Mode()&0700 != 0700) {
		log.Fatalf("Shadowsysfs: Invalid %s setting: %s: Permissions do not include 0700", envvar, d)
	}
}

func shadowFetchActiveShadows() ([]string, error) {
	var shadowdirs []string

	_, err := os.Stat(shadowbasedir)
	if err != nil && os.IsNotExist(err) {
		return shadowdirs, nil
	}

	files, err := ioutil.ReadDir(shadowbasedir)
	if err != nil {
		log.Printf("Shadowsysfs: Can't read directory %s: %s\n", shadowbasedir, err)
		return nil, fmt.Errorf("Shadowsysfs: Can't read directory %s: %s", shadowbasedir, err)
	}

	for _, f := range files {
		match, _ := regexp.MatchString("sysfs-apqn-.*", f.Name())
		if match {
			shadowdirs = append(shadowdirs, f.Name())
		}
	}

	return shadowdirs, nil
}
