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
 * kubernetes device plugin manager implementation
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/kubevirt/device-plugin-manager/pkg/dpm"
	kdp "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type ZMdevDPMLister struct {
	machineid    string
	setnameslist []string
}

type ZMdevResPlugin struct {
	resource    string
	lister      *ZMdevDPMLister
	ccset       *CryptoConfigSet
	tag         []byte
	apqns       APQNList
	devices     []*kdp.Device
	changedChan chan struct{}
	stopChan    chan struct{}
}

//func (p *ZMdevResPlugin) initDevices() {
//	p.devices = []*kdp.Device{}
//	for _, setname := range p.lister.setnameslist {
//		p.devices = append(p.devices, &kdp.Device{
//			ID:     setname,
//			Health: kdp.Healthy, // Default to healthy
//		})
//	}
//	log.Printf("ZMdevResPlugin['%s']: Initialized devices: %v\n", p.resource, p.devices)
//}

func (p *ZMdevResPlugin) filterAPQNs(ccset *CryptoConfigSet, apqnlist APQNList) APQNList {
	var apqns APQNList
	if ccset == nil {
		return apqns
	}

	for _, a := range apqnlist {
		for _, c := range ccset.APQNDefs {
			if a.Adapter != c.Adapter || a.Domain != c.Domain {
				continue
			}
			if len(c.MachineId) > 0 && p.lister.machineid != c.MachineId {
				continue
			}
			if len(ccset.MinCexGen) > 0 && a.Gen < ccset.MinCexGen {
				log.Printf("MDEV Plugin['%s']: APQN (%d,%d) not announced. Card generation = %s, but %s or higher required for this config set\n",
					p.resource, a.Adapter, a.Domain, a.Gen, ccset.MinCexGen)
				continue
			}
			apqns = append(apqns, a)
		}
	}

	return apqns
}

func (p *ZMdevResPlugin) makePluginDevsFromAPQNs() []*kdp.Device {

	var devices []*kdp.Device

	if p.ccset.Overcommit <= 0 {
		p.ccset.Overcommit = apqnOverCommitLimit
		log.Printf("MDEV Plugin['%s']: Overcommit not specified in ConfigSet, fallback to %d \n",
			p.resource, apqnOverCommitLimit)
	}

	for _, a := range p.apqns {
		health := kdp.Healthy
		if !a.Online {
			health = kdp.Unhealthy
		}
		for i := 0; i < p.ccset.Overcommit; i++ {
			devices = append(devices, &kdp.Device{
				ID:     fmt.Sprintf(ApqnFmtStr, a.Adapter, a.Domain, i),
				Health: health,
			})
		}
	}

	return devices
}

func (p *ZMdevResPlugin) checkChanged() bool {

	//log.Printf("MDEV Plugin['%s']: checkChanged() rescanning available APQNs\n", p.resource)

	var apqnsChanged, configChanged bool
	ccset, tag := GetCurrentCryptoConfigSet(p.ccset, p.resource, p.tag)

	allnodeapqns, err := apScanAPQNs(false)
	if err != nil {
		log.Printf("MDEV Plugin['%s']: failure trying to rescan node APQNs: %s\n", p.resource, err)
		return false
	}

	// check for change in APQNs
	apqns := p.filterAPQNs(ccset, allnodeapqns)
	if !apEqualAPQNLists(apqns, p.apqns) {
		log.Printf("MDEV Plugin['%s']: Rescan found %d eligible APQNs (with changes): %s\n",
			p.resource, len(apqns), apqns)
		apqnsChanged = true
	}

	// check for change in ConfigSet (currently only overcommit limit)
	if ccset != nil && ccset.Overcommit != p.ccset.Overcommit {
		log.Printf("MDEV Plugin['%s']: Rescan found changes in ConfigSet: overcommit limit has changed\n",
			p.resource)
		configChanged = true
	}

	if apqnsChanged || configChanged {
		p.ccset, p.tag = ccset, tag
		p.apqns = apqns
		p.devices = p.makePluginDevsFromAPQNs()
		log.Printf("MDEV Plugin['%s']: Derived %d plugin devices from the list of APQNs\n",
			p.resource, len(p.devices))
		return true
	} else {
		log.Printf("MDEV Plugin['%s']: no changes\n", p.resource)
		return false
	}
}

func (p *ZMdevResPlugin) checkChangedLoop() {

	tick := time.NewTicker(apqnsCheckInterval * time.Second)

ForLoop:
	for {
		select {
		case <-p.stopChan:
			tick.Stop()
			break ForLoop
		case <-tick.C:
			if p.checkChanged() {
				p.changedChan <- struct{}{}
			}
		}
	}
}

// Implement required methods for ZMdevResPlugin (similar to ZCryptoResPlugin)
func (p *ZMdevResPlugin) Start() error {
	log.Printf("ZMdevResPlugin['%s']: Start()\n", p.resource)

	allnodeapqns, err := apScanAPQNs(false)
	if err != nil {
		log.Printf("MDEV Plugin['%s']: failure trying to scan node APQNs: %s\n", p.resource, err)
		return fmt.Errorf("MDEV Plugin['%s']: fatal failure at start", p.resource)
	}

	p.apqns = p.filterAPQNs(p.ccset, allnodeapqns)
	log.Printf("MDEV Plugin['%s']: Found %d eligible APQNs: %s\n", p.resource, len(p.apqns), p.apqns)

	p.devices = p.makePluginDevsFromAPQNs()
	log.Printf("MDEV Plugin['%s']: Derived %d plugin devices from the list of APQNs\n",
		p.resource, len(p.devices))

	// Mock device setup or other initialization logic
	p.stopChan = make(chan struct{})
	p.changedChan = make(chan struct{})

	go p.checkChangedLoop()

	return nil
}

func (p *ZMdevResPlugin) Stop() error {
	log.Printf("ZMdevResPlugin['%s']: Stop()\n", p.resource)

	close(p.stopChan)
	close(p.changedChan)
	return nil
}

func zcryptCreateMDevNode(apqn string) (string, error) {
	const (
		devBase        = "/dev/vfio"
		sysBusBase     = "/sys/bus/ap"
		sysDeviceBase  = "/sys/devices/vfio_ap/matrix"
		commandFile    = "mdev_supported_types/vfio_ap-passthrough/create"
	)

	// Validate APQN format
	if !strings.Contains(apqn, ".") || len(apqn) < 7 {
		return "", fmt.Errorf("incorrect format for APQN: %s", apqn)
	}

	// Extract APID and APQI
	parts := strings.Split(apqn, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("incorrect format for APQN: %s", apqn)
	}

	apid := strings.TrimLeft(parts[0], "0")
	if apid == "" {
		apid = "0"
	}
	apqi := strings.TrimLeft(parts[1], "0")
	if apqi == "" {
		apqi = "0"
	}

	// Release the device from the host
	if err := writeToFile(filepath.Join(sysBusBase, "apmask"), fmt.Sprintf("-0x%s", apid)); err != nil {
		return "", fmt.Errorf("failed to update apmask: %w", err)
	}
	if err := writeToFile(filepath.Join(sysBusBase, "aqmask"), fmt.Sprintf("-0x%s", apqi)); err != nil {
		return "", fmt.Errorf("failed to update aqmask: %w", err)
	}

	// Create a mediated device (mdev)
	commandPath := filepath.Join(sysDeviceBase, commandFile)
	if _, err := os.Stat(commandPath); os.IsNotExist(err) {
		return "", fmt.Errorf("command file not found: %s", commandPath)
	}

	mdevUUID := uuid.New().String()
	if err := writeToFile(commandPath, mdevUUID); err != nil {
		return "", fmt.Errorf("failed to create mediated device: %w", err)
	}

	// Verify the mediated device
	mdevPath := filepath.Join(sysDeviceBase, mdevUUID)
	if _, err := os.Stat(filepath.Join(mdevPath, "iommu_group")); os.IsNotExist(err) {
		return "", fmt.Errorf("iommu_group not found for mdev: %s", mdevUUID)
	}

	devIndex, err := readLink(filepath.Join(mdevPath, "iommu_group"))
	if err != nil || devIndex == "" {
		return "", fmt.Errorf("failed to get dev_index for mdev: %s", mdevUUID)
	}

	// Assign adapter and domain
	if err := writeToFile(filepath.Join(mdevPath, "assign_adapter"), fmt.Sprintf("0x%s", apid)); err != nil {
		return "", fmt.Errorf("failed to assign adapter: %w", err)
	}
	if err := writeToFile(filepath.Join(mdevPath, "assign_domain"), fmt.Sprintf("0x%s", apqi)); err != nil {
		return "", fmt.Errorf("failed to assign domain: %w", err)
	}

	return fmt.Sprintf("%s/%s", devBase, devIndex), nil
}

// Utility functions

func writeToFile(path, content string) error {
	return ioutil.WriteFile(path, []byte(content), 0644)
}

func readLink(path string) (string, error) {
	link, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	return filepath.Base(link), nil
}

func (p *ZMdevResPlugin) Allocate(ctx context.Context, req *kdp.AllocateRequest) (*kdp.AllocateResponse, error) {
	log.Printf("ZMdevResPlugin['%s']: Allocate(request=%v)\n", p.resource, req)

	rsp := new(kdp.AllocateResponse)
	for _, careq := range req.GetContainerRequests() {
		carsp := kdp.ContainerAllocateResponse{}
		for _, id := range careq.GetDevicesIDs() {
			var card, queue, overcount int
			n, err := fmt.Sscanf(id, ApqnFmtStr, &card, &queue, &overcount)
			if err != nil || n < 3 {
				log.Printf("MDEV Plugin['%s']: Error parsing device id '%s'\n", p.resource, id)
				return nil, fmt.Errorf("Error parsing device id '%s'", id)
			}
			znode := fmt.Sprintf("zcrypt-"+ApqnFmtStr, card, queue, overcount)
			log.Printf("MDEV Plugin['%s']: creating zcrypt device node '%s'\n", p.resource, znode)
			// Create a mediated device
			apqn := fmt.Sprintf("%02x.%04x", card, queue)
			mdev_path, err := zcryptCreateMDevNode(apqn)
			if err != nil {
				log.Printf("MDEV Plugin['%s']: Error creating zcrypt node '%s': %s\n", p.resource, znode, err)
				return nil, fmt.Errorf("Error creating zcrypt node '%s'", znode)
			}
			log.Printf("MDEV Plugin['%s']: creating mediated device node '%s'\n", p.resource, mdev_path)
			// mdev_path should look like "/dev/vfio/0"
			dev := &kdp.DeviceSpec{
				HostPath:      mdev_path,
				ContainerPath: mdev_path,
				Permissions:   "rw",
			}
			carsp.Devices = append(carsp.Devices, dev)
		}
		rsp.ContainerResponses = append(rsp.ContainerResponses, &carsp)
	}
	log.Printf("MDEV Plugin['%s']: Allocate() response=%v\n", p.resource, rsp)
	return rsp, nil
}

func (m *ZMdevDPMLister) GetResourceNamespace() string {
	log.Printf("MDEV Plugin: Announcing 'mdev.s390.ibm.com' as our resource namespace for ZMdev\n")
	return "mdev.s390.ibm.com"
}

func areTheseSortedStringListsEqual(l1, l2 []string) bool {
	if len(l1) != len(l2) {
		return false
	}
	for i, _ := range l1 {
		if l1[i] != l2[i] {
			return false
		}
	}
	return true
}

func createMockDevice(setname string) error {
	mockDevicePath := fmt.Sprintf("/tmp/mock-%s", setname)

	// Check if the device node already exists
	if _, err := os.Stat(mockDevicePath); os.IsNotExist(err) {
		log.Printf("Failed to find a device node at %s", mockDevicePath)
	} else {
		log.Printf("Find a mock device node at %s", mockDevicePath)
	}
	return nil
}

func (z *ZMdevDPMLister) Discover(nameslistchan chan dpm.PluginNameList) {

	areTheseSortedStringListsEqual := func(l1, l2 []string) bool {
		if len(l1) != len(l2) {
			return false
		}
		for i, _ := range l1 {
			if l1[i] != l2[i] {
				return false
			}
		}
		return true
	}

	// prepare and announce the initial list of crypto config setnames
	sets := GetCurrentCryptoConfig().GetListOfSetNames()
	sort.Strings(sets)
	z.setnameslist = sets
	log.Printf("MDEV Plugin: Register plugins for these CryptoConfigSets: %v\n", z.setnameslist)
	nameslistchan <- dpm.PluginNameList(z.setnameslist)

	// every Cccheckinterval seconds check if the list of setnames has changed
	tick := time.NewTicker(Cccheckinterval * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-nameslistchan:
			return
		case t := <-tick.C:
			if t.IsZero() {
				return
			}
			sets = GetCurrentCryptoConfig().GetListOfSetNames()
			sort.Strings(sets)
			if !areTheseSortedStringListsEqual(sets, z.setnameslist) {
				z.setnameslist = sets
				log.Printf("MDEV Plugin: Found crypto config set changes. Reannouncing: %v\n", z.setnameslist)
				nameslistchan <- dpm.PluginNameList(z.setnameslist)
			} else if len(z.setnameslist) == 0 {
				log.Printf("MDEV Plugin: No crypto config sets available, check configuration !\n")
			}
		}
	}
}

func (m *ZMdevDPMLister) NewPlugin(resource string) dpm.PluginInterface {
	log.Printf("ZMdevDPMLister: NewPlugin('%s')\n", resource)

	ccset, tag := GetCurrentCryptoConfigSet(nil, resource, nil)

	// Create a new instance of the resource plugin
	return &ZMdevResPlugin{
		lister:   m,
		resource: resource,
		ccset:    ccset,
		tag:      tag,
	}
}

func (p *ZMdevResPlugin) GetDevicePluginOptions(ctx context.Context, req *kdp.Empty) (*kdp.DevicePluginOptions, error) {
	log.Printf("ZMdevResPlugin['%s']: GetDevicePluginOptions()\n", p.resource)

	// Return default options (no pre-start required)
	return &kdp.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

func (p *ZMdevResPlugin) ListAndWatch(e *kdp.Empty, s kdp.DevicePlugin_ListAndWatchServer) error {
	log.Printf("MDEV Plugin['%s']: ListAndWatch() Announcing %d devices: %s\n",
		p.resource, len(p.devices), p.devices)
	s.Send(&kdp.ListAndWatchResponse{Devices: p.devices})

	for {
		select {
		case <-p.stopChan:
			return nil
		case _, ok := <-p.changedChan:
			if !ok {
				return nil
			}
			log.Printf("MDEV Plugin['%s']: ListAndWatch() Re-announcing %d devices: %s\n",
				p.resource, len(p.devices), p.devices)
			s.Send(&kdp.ListAndWatchResponse{Devices: p.devices})
		}
	}
}

func (p *ZMdevResPlugin) GetPreferredAllocation(ctx context.Context,
	req *kdp.PreferredAllocationRequest) (*kdp.PreferredAllocationResponse, error) {

	//log.Printf("MDEV Plugin['%s']: GetPreferredAllocation()\n", p.resource)

	return nil, nil
}

func (p *ZMdevResPlugin) PreStartContainer(context.Context, *kdp.PreStartContainerRequest) (*kdp.PreStartContainerResponse, error) {

	//log.Printf("MDEV Plugin['%s']: PreStartContainer()\n", p.resource)
	return nil, fmt.Errorf("PreStartContainer() not implemented")
}

func RunZMdevResPlugins() {

	machineid, err := ccGetMachineId()
	if err != nil {
		log.Fatalf("MDEV Plugin: Fetching machine id failed: %s\n", err)
	}

	zmdevLister := &ZMdevDPMLister{
		machineid: machineid,
	}

	mgr := dpm.NewManager(zmdevLister)
	mgr.Run()
}

