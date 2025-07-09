/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pravk03/topologyutil/pkg/cpuinfo"
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	"k8s.io/utils/cpuset"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
)

func createCPUDevices() []resourceapi.Device {

	cpuInfo, err := cpuinfo.GetCPUInfos()
	if err != nil {
		klog.Errorf("error getting CPU topology: %v", err)
		return nil
	}
	klog.V(5).Infof("CPU topology: %+v", cpuInfo)
	devices := []resourceapi.Device{}

	for _, cpu := range cpuInfo {
		cpuDevice := resourceapi.Device{
			Basic: &resourceapi.BasicDevice{
				Attributes: make(map[resourceapi.QualifiedName]resourceapi.DeviceAttribute),
				Capacity:   make(map[resourceapi.QualifiedName]resourceapi.DeviceCapacity),
			},
		}
		cpuDevice.Name = fmt.Sprintf("cpu%d", cpu.CpuId)
		cpuID := int64(cpu.CpuId)
		coreID := int64(cpu.CoreId)
		numaNode := int64(cpu.NumaNode)
		cpuDevice.Basic.Attributes["cpuID"] = resourceapi.DeviceAttribute{IntValue: &cpuID}
		cpuDevice.Basic.Attributes["coreID"] = resourceapi.DeviceAttribute{IntValue: &coreID}
		cpuDevice.Basic.Attributes["numaNode"] = resourceapi.DeviceAttribute{IntValue: &numaNode}
		cpuDevice.Basic.Attributes["numaAffinityMask"] = resourceapi.DeviceAttribute{StringValue: &cpu.NumaNodeAffinityMask}
		devices = append(devices, cpuDevice)
	}
	return devices

}

func (cp *CPUDriver) PublishResources(ctx context.Context) {
	klog.Infof("Publishing resources")

	cpuDevices := createCPUDevices()
	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			cp.nodeName: {Slices: []resourceslice.Slice{{Devices: cpuDevices}}}},
	}
	err := cp.draPlugin.PublishResources(ctx, resources)
	if err != nil {
		klog.Errorf("error publishing resources: %v", err)
	}
}

func (cp *CPUDriver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.Infof("PrepareResourceClaims is called: number of claims: %d", len(claims))

	result := make(map[types.UID]kubeletplugin.PrepareResult)

	if len(claims) == 0 {
		return result, nil
	}

	for _, claim := range claims {
		result[claim.UID] = cp.prepareResourceClaim(ctx, claim)
	}
	return result, nil
}

func getCDIDeviceName(uid types.UID) string {
	return fmt.Sprintf("claim-%s", uid)
}

func (cp *CPUDriver) prepareResourceClaim(_ context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	klog.Infof("prepareResourceClaim claim:%s/%s", claim.Namespace, claim.Name)

	claimCPUIDs := []int{}
	for _, alloc := range claim.Status.Allocation.Devices.Results {
		if alloc.Driver != cp.driverName {
			continue
		}
		cpuID, err := strconv.Atoi(strings.TrimPrefix(alloc.Device, "cpu"))
		if err != nil {
			return kubeletplugin.PrepareResult{
				Err: fmt.Errorf("error parsing CPU ID from device %q for claim %s: %v", alloc.Device, claim.Name, err),
			}
		}
		claimCPUIDs = append(claimCPUIDs, cpuID)
	}
	claimCPUSet := cpuset.New(claimCPUIDs...)

	if claimCPUSet.IsEmpty() {
		klog.Errorf("prepareResourceClaim claim:%s/%s has no CPU allocations", claim.Namespace, claim.Name)
		return kubeletplugin.PrepareResult{}
	}

	deviceName := getCDIDeviceName(claim.UID)
	envVar := fmt.Sprintf("%s_claim_%s=%s", cdiEnvVarPrefix, claim.Name, claimCPUSet.String())
	if err := cp.cdiMgr.AddDevice(deviceName, envVar); err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}

	qualifiedName := cdiparser.QualifiedName(cdiVendor, cdiClass, deviceName)
	preparedDevices := []kubeletplugin.Device{}
	for _, allocResult := range claim.Status.Allocation.Devices.Results {
		preparedDevice := kubeletplugin.Device{
			PoolName:     allocResult.Pool,
			DeviceName:   allocResult.Device,
			CDIDeviceIDs: []string{qualifiedName},
		}
		preparedDevices = append(preparedDevices, preparedDevice)
	}

	return kubeletplugin.PrepareResult{
		Devices: preparedDevices,
	}
}

func (np *CPUDriver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))

	result := make(map[types.UID]error)

	if len(claims) == 0 {
		return result, nil
	}

	for _, claim := range claims {
		klog.Infof("UnprepareResourceClaims claim:%+v", claim)
		err := np.unprepareResourceClaim(ctx, claim)
		result[claim.UID] = err
		if err != nil {
			klog.Infof("error unpreparing ressources for claim %s/%s : %v", claim.Namespace, claim.Name, err)
		}
	}
	return result, nil
}

func (cp *CPUDriver) unprepareResourceClaim(_ context.Context, claim kubeletplugin.NamespacedObject) error {
	// Remove the device from the CDI spec file using the manager.
	return cp.cdiMgr.RemoveDevice(getCDIDeviceName(claim.UID))
}
