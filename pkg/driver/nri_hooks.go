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
	"encoding/json"
	"strings"

	"github.com/containerd/nri/pkg/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/klog/v2"
)

func (cp *CPUDriver) Synchronize(_ context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	klog.Infof("Synchronized state with the runtime (%d pods, %d containers)...",
		len(pods), len(containers))

	for _, pod := range pods {
		klog.Infof("Synchronize Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	}

	return nil, nil
}

// CreateContainer handles container creation requests.
func (cp *CPUDriver) CreateContainer(_ context.Context, pod *api.PodSandbox, ctr *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	klog.Infof("CreateContainer Pod:%s/%s PodUID:%s Container:%s ContainerID:%s", pod.Namespace, pod.Name, pod.Uid, ctr.Name, ctr.Id)

	adjust := &api.ContainerAdjustment{}

	// This is a HACK: There is no way to map a Resource claim to a contianer in the dra hook.
	// The claim.Status.ReservedFor conly contains the podUID.
	// There is no direct way to get the claim or the claim request from the PodSandbox or Container objects in the NRI hook.
	for name, value := range pod.Annotations {
		if name == "kubectl.kubernetes.io/last-applied-configuration" {
			klog.Infof("Pod Annotation %s: %s", name, value)
			var podSpec v1.Pod
			err := json.Unmarshal([]byte(value), &podSpec)
			if err == nil {
				// klog.Infof("Pod Annotation Json:%v", podSpec)
				for _, podContainer := range podSpec.Spec.Containers {
					klog.Infof("Pod Container %s", podContainer.Name)
					if podContainer.Name == ctr.Name {
						klog.Infof("Pod Container %s matches CreateContainer", podContainer.Name)
						for _, claim := range podContainer.Resources.Claims {
							klog.Infof("Pod Container %s Claim: %+v", podContainer.Name, claim.Name)
							cpuIDs, found := cp.podConfigStore.GetClaimRequestCPUs(types.UID(pod.Uid), claim.Name)
							klog.Infof("Pod Container %s Claim: %v found:%v cpuIDs:%v", podContainer.Name, claim.Name, found, cpuIDs)
							adjust.Linux = &api.LinuxContainerAdjustment{
								Resources: &api.LinuxResources{
									Cpu: &api.LinuxCPU{
										Cpus: strings.Join(cpuIDs, ","),
									},
								},
							}
							klog.Infof("Pod Container %s adjust:%+v", podContainer.Name, adjust)
						}
					}
				}
			} else {
				klog.Errorf("Error unmarshalling pod annotation %s: %v", name, err)
			}
		}
	}
	return adjust, nil, nil
}

func (cp *CPUDriver) RunPodSandbox(ctx context.Context, pod *api.PodSandbox) error {
	klog.Infof("RunPodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	return nil
}

func (cp *CPUDriver) StopPodSandbox(ctx context.Context, pod *api.PodSandbox) error {
	klog.Infof("StopPodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	return nil
}

func (cp *CPUDriver) RemovePodSandbox(_ context.Context, pod *api.PodSandbox) error {
	klog.Infof("RemovePodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	return nil
}
