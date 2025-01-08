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
	devices     []*kdp.Device
	changedChan chan struct{}
	stopChan    chan struct{}
}

func (p *ZMdevResPlugin) initDevices() {
	p.devices = []*kdp.Device{}
	for _, setname := range p.lister.setnameslist {
		p.devices = append(p.devices, &kdp.Device{
			ID:     setname,
			Health: kdp.Healthy, // Default to healthy
		})
	}
	log.Printf("ZMdevResPlugin['%s']: Initialized devices: %v\n", p.resource, p.devices)
}

// Implement required methods for ZMdevResPlugin (similar to ZCryptoResPlugin)
func (p *ZMdevResPlugin) Start() error {
	log.Printf("ZMdevResPlugin['%s']: Start()\n", p.resource)

	// Mock device setup or other initialization logic
	p.stopChan = make(chan struct{})
	p.changedChan = make(chan struct{})
	p.initDevices()
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
				log.Printf("Plugin['%s']: Error parsing device id '%s'\n", p.resource, id)
				return nil, fmt.Errorf("Error parsing device id '%s'", id)
			}
			znode := fmt.Sprintf("zcrypt-"+ApqnFmtStr, card, queue, overcount)
			log.Printf("Plugin['%s']: creating zcrypt device node '%s'\n", p.resource, znode)
			// Create a mediated device
			apqn := fmt.Sprintf("%02x.%04x", card, queue)
			mdev_path, err := zcryptCreateMDevNode(apqn)
			if err != nil {
				log.Printf("Plugin['%s']: Error creating zcrypt node '%s': %s\n", p.resource, znode, err)
				return nil, fmt.Errorf("Error creating zcrypt node '%s'", znode)
			}
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
	log.Printf("Plugin['%s']: Allocate() response=%v\n", p.resource, rsp)
	return rsp, nil
}

func (m *ZMdevDPMLister) GetResourceNamespace() string {
	log.Printf("Plugin: Announcing 'mdev.s390.ibm.com' as our resource namespace for ZMdev\n")
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
	// Define a static list of set names
	staticSetNames := []string{"CCA_for_customer_1", "EP11_for_customer_1"}
	sort.Strings(staticSetNames) // Ensure the list is sorted
	z.setnameslist = staticSetNames

	// Mock a device for each static set name
	for _, setname := range staticSetNames {
		err := createMockDevice(setname)
		if err != nil {
			log.Printf("Failed to create mock device for setname '%s': %v", setname, err)
			continue
		}
	}

	// Announce the static set names as resources
	log.Printf("Plugin: Registering resources for these static set names: %v\n", z.setnameslist)
	nameslistchan <- dpm.PluginNameList(z.setnameslist)
}

func (m *ZMdevDPMLister) NewPlugin(resource string) dpm.PluginInterface {
	log.Printf("ZMdevDPMLister: NewPlugin('%s')\n", resource)

	// Create a new instance of the resource plugin
	return &ZMdevResPlugin{
		resource: resource,
		lister:   m,
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
	log.Printf("Plugin['%s']: ListAndWatch() Announcing %d devices: %s\n",
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
			log.Printf("Plugin['%s']: ListAndWatch() Re-announcing %d devices: %s\n",
				p.resource, len(p.devices), p.devices)
			s.Send(&kdp.ListAndWatchResponse{Devices: p.devices})
		}
	}
}

func (p *ZMdevResPlugin) GetPreferredAllocation(ctx context.Context,
	req *kdp.PreferredAllocationRequest) (*kdp.PreferredAllocationResponse, error) {

	//log.Printf("Plugin['%s']: GetPreferredAllocation()\n", p.resource)

	return nil, nil
}

func (p *ZMdevResPlugin) PreStartContainer(context.Context, *kdp.PreStartContainerRequest) (*kdp.PreStartContainerResponse, error) {

	//log.Printf("Plugin['%s']: PreStartContainer()\n", p.resource)
	return nil, fmt.Errorf("PreStartContainer() not implemented")
}

func RunZMdevResPlugins() {

	machineid, err := ccGetMachineId()
	if err != nil {
		log.Fatalf("Plugin: Fetching machine id failed: %s\n", err)
	}

	zmdevLister := &ZMdevDPMLister{
		machineid: machineid,
	}

	mgr := dpm.NewManager(zmdevLister)
	mgr.Run()
}

