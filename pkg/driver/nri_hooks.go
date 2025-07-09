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

	"github.com/containerd/nri/pkg/api"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/cpuset"

	"k8s.io/klog/v2"
)

func (cp *CPUDriver) Synchronize(ctx context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	klog.Infof("Synchronized state with the runtime (%d pods, %d containers)...",
		len(pods), len(containers))

	for _, pod := range pods {
		klog.Infof("Synchronize Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
		for _, container := range containers {
			cpus := container.GetLinux().GetResources().GetCpu().GetCpus()
			klog.Infof("Synchronize container %s cpus:%s", container.Name, cpus)
			// TODO(pravk03): Restart situations needs to be handled here.
			// Ran into rate limiting issues when trying to use pod resource API for fetching the resource claim info here.
		}
	}

	return nil, nil
}

func parseDRAEnvToCPUSet(envs []string) (cpuset.CPUSet, error) {
	guaranteedCPUs := cpuset.New()
	for _, env := range envs {
		if !strings.HasPrefix(env, cdiEnvVarPrefix) {
			continue
		}

		keyVal := strings.SplitN(env, "=", 2)
		if len(keyVal) != 2 {
			klog.Errorf("Malformed DRA env entry %q, expected format: %s_key=value", env, cdiEnvVarPrefix)
			continue
		}
		cpuSetValue := keyVal[1]

		parsedSet, err := cpuset.Parse(cpuSetValue)
		if err != nil {
			return cpuset.New(), fmt.Errorf("failed to parse cpuset value %q from env %q: %w", cpuSetValue, env, err)
		}

		guaranteedCPUs = guaranteedCPUs.Union(parsedSet)
	}

	return guaranteedCPUs, nil
}

// CreateContainer handles container creation requests.
func (cp *CPUDriver) CreateContainer(_ context.Context, pod *api.PodSandbox, ctr *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	klog.Infof("CreateContainer Pod:%s/%s PodUID:%s Container:%s ContainerID:%s", pod.Namespace, pod.Name, pod.Uid, ctr.Name, ctr.Id)
	adjust := &api.ContainerAdjustment{}
	updates := []*api.ContainerUpdate{}

	guaranteedCPUs, err := parseDRAEnvToCPUSet(ctr.Env)
	if err != nil {
		klog.Errorf("Error parsing DRA env for container %s in pod %s/%s: %v", ctr.Name, pod.Namespace, pod.Name, err)
	}

	if guaranteedCPUs.IsEmpty() {
		// No guaranteed CPUs found, use public CPUs.
		publicCPUs := cp.podConfigStore.GetPublicCPUs()
		klog.Infof("No guaranteed CPUs found in DRA env for pod %s/%s container %s. Using public CPUs %s", pod.Namespace, pod.Name, ctr.Name, publicCPUs.String())
		adjust.SetLinuxCPUSetCPUs(publicCPUs.String())
		cp.podConfigStore.SetSharedContainerState(types.UID(pod.Uid), ctr.Name, types.UID(ctr.Id))
	} else {
		klog.Infof("Guaranteed CPUs found for pod:%scontainer:%s with cpus:%v", pod.Name, ctr.Name, guaranteedCPUs.String())
		adjust.SetLinuxCPUSetCPUs(guaranteedCPUs.String())
		cp.podConfigStore.SetGuaranteedContainerState(types.UID(pod.Uid), ctr.Name, guaranteedCPUs)
		// Remove the guaranteed CPUs from the remaining contianers
		publicCPUs := cp.podConfigStore.GetPublicCPUs()
		sharedCPUContainers := cp.podConfigStore.GetContainersWithSharedCPUs()
		klog.Infof("Updating best-effort contianer's CPUs (public CPUs) to %+v", publicCPUs.String())
		for _, containerUID := range sharedCPUContainers {
			containerUpdate := &api.ContainerUpdate{
				ContainerId: string(containerUID),
			}
			containerUpdate.SetLinuxCPUSetCPUs(publicCPUs.String())
			updates = append(updates, containerUpdate)
		}
	}
	return adjust, updates, nil
}

func (cp *CPUDriver) RemoveContainer(_ context.Context, pod *api.PodSandbox, ctr *api.Container) error {
	// TODO(pravk03): Handle contianer removals and restarts
	return nil
}

func (cp *CPUDriver) RunPodSandbox(_ context.Context, pod *api.PodSandbox) error {
	klog.Infof("RunPodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	return nil
}

func (cp *CPUDriver) StopPodSandbox(_ context.Context, pod *api.PodSandbox) error {
	klog.Infof("StopPodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	return nil
}

func (cp *CPUDriver) RemovePodSandbox(_ context.Context, pod *api.PodSandbox) error {
	klog.Infof("RemovePodSandbox Pod %s/%s UID %s", pod.Namespace, pod.Name, pod.Uid)
	// TODO(pravk03): Handle public CPU updates for remaining contianers
	cp.podConfigStore.DeletePod(types.UID(pod.Uid))
	return nil
}
