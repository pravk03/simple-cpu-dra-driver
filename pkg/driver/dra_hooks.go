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
	"strings"

	"github.com/pravk03/topologyutil/pkg/cpuinfo"
	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	return
}

func (cp *CPUDriver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.Infof("PrepareResourceClaims is called: number of claims: %d", len(claims))

	if len(claims) == 0 {
		return nil, nil
	}
	result := make(map[types.UID]kubeletplugin.PrepareResult)
	for _, claim := range claims {
		result[claim.UID] = cp.prepareResourceClaim(ctx, claim)
	}
	return result, nil
}

func (cp *CPUDriver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	claimNamespacedName := types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}
	klog.V(2).Infof("PrepareResourceClaim for Claim %v", claimNamespacedName)

	for _, reserved := range claim.Status.ReservedFor {
		// Currently there is no way to map a resource claim to a contianer in the dra hook.
		// The claim.Status.ReservedFor only contains the podUID.
		// TODO(pravk03): Explore a way to map container to resource claim without making API request. We may need to use CDI here.
		pod, err := cp.kubeClient.CoreV1().Pods(claim.Namespace).Get(ctx, reserved.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Error getting pod %s/%s for claim %s: %v", claim.Namespace, reserved.Name, claim.Name, err)
			continue
		}

		podResourceClaimNameMap := make(map[string]string)
		for _, podClaim := range pod.Spec.ResourceClaims {
			podResourceClaimNameMap[podClaim.Name] = *podClaim.ResourceClaimName
		}

		containersWithClaim := []string{}
		for _, container := range pod.Spec.Containers {
			for _, rClaim := range container.Resources.Claims {
				if podResourceClaimNameMap[rClaim.Name] == claim.Name {
					containersWithClaim = append(containersWithClaim, container.Name)
				}
			}
		}
		for _, initContainer := range pod.Spec.InitContainers {
			for _, rClaim := range initContainer.Resources.Claims {
				if podResourceClaimNameMap[rClaim.Name] == claim.Name {
					containersWithClaim = append(containersWithClaim, initContainer.Name)
				}
			}
		}
		klog.Infof("prepareResourceClaim claim:%+v containersWithClaim:%+v", claimNamespacedName, containersWithClaim)
		claimCPUIDs := []string{}
		for _, deviceAlloc := range claim.Status.Allocation.Devices.Results {
			if deviceAlloc.Driver != cp.driverName {
				continue
			}
			numericID := strings.TrimPrefix(deviceAlloc.Device, "cpu")
			claimCPUIDs = append(claimCPUIDs, numericID)
		}
		klog.Infof("prepareResourceClaim claim:%+v claimCPUIDs:%+v", claimNamespacedName, claimCPUIDs)

		for _, container := range containersWithClaim {
			cp.podConfigStore.SetContainerCPUs(reserved.UID, container, claimNamespacedName, claimCPUIDs)
		}
	}
	return kubeletplugin.PrepareResult{}
}

func (np *CPUDriver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))
	if len(claims) == 0 {
		return nil, nil
	}

	result := make(map[types.UID]error)
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
	cp.podConfigStore.DeleteClaim(claim.NamespacedName)
	return nil
}
