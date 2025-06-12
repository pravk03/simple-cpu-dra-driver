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

	"github.com/pravk03/topologyutil/pkg/cpuinfo"
	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
)

const (
	rdmaCmPath = "/dev/infiniband/rdma_cm"
)

func createCPUDevices() []resourceapi.Device {

	cpuInfo, err := cpuinfo.GetCPUInfos()
	if err != nil {
		klog.Errorf("error getting CPU topology: %v", err)
		return nil
	}
	klog.Infof("CPU topology: %v", cpuInfo)
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
	klog.V(2).Infof("Publishing resources")

	cpuDevices := createCPUDevices()
	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			cp.nodeName: {Slices: []resourceslice.Slice{{Devices: cpuDevices}}}},
	}
	err := cp.draPlugin.PublishResources(ctx, resources)
	if err != nil {
		klog.Errorf("error publishing resources: %v", err)
	}
	return
}

func (cp *CPUDriver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.V(2).Infof("PrepareResourceClaims is called: number of claims: %d", len(claims))

	if len(claims) == 0 {
		return nil, nil
	}
	result := make(map[types.UID]kubeletplugin.PrepareResult)

	for _, claim := range claims {
		klog.V(2).Infof("NodePrepareResources: Claim Request %s/%s", claim.Namespace, claim.Name)
		result[claim.UID] = cp.prepareResourceClaim(ctx, claim)
	}
	return result, nil
}

func (cp *CPUDriver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	klog.V(2).Infof("PrepareResourceClaim Claim %s/%s", claim.Namespace, claim.Name)
	return kubeletplugin.PrepareResult{}
}

func (np *CPUDriver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.V(2).Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))
	if len(claims) == 0 {
		return nil, nil
	}

	result := make(map[types.UID]error)
	for _, claim := range claims {
		err := np.unprepareResourceClaim(ctx, claim)
		result[claim.UID] = err
		if err != nil {
			klog.Infof("error unpreparing ressources for claim %s/%s : %v", claim.Namespace, claim.Name, err)
		}
	}
	return result, nil
}

func (cp *CPUDriver) unprepareResourceClaim(_ context.Context, claim kubeletplugin.NamespacedObject) error {
	return nil
}
