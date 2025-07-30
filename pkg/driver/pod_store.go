/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driver

import (
	"sync"

	"github.com/pravk03/dracpu/pkg/cpuinfo"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/cpuset"
)

// CPUType defines whether the CPU allocation is for a Guaranteed or BestEffort container.
type CPUType string

const (
	CPUTypeGuaranteed CPUType = "Guaranteed"
	CPUTypeShared     CPUType = "Shared"
)

// ContainerCPUState holds the CPU allocation type and all claim assignments for a container.
type ContainerCPUState struct {
	Type          CPUType
	ContainerName string
	ContainerUID  types.UID
	// valid only when Type is CPUTypeGuaranteed
	guaranteedCPUs cpuset.CPUSet
}

// PodCPUAssignments maps a container name to its CPU state.
type PodCPUAssignments map[string]*ContainerCPUState

// PodConfigStore maps a Pod's UID directly to its container-level CPU assignments.
type PodConfigStore struct {
	mu         sync.RWMutex
	configs    map[types.UID]PodCPUAssignments
	allCPUs    cpuset.CPUSet
	publicCPUs cpuset.CPUSet
}

func NewPodConfigStore() *PodConfigStore {
	cpuIDs := []int{}
	cpuInfo, err := cpuinfo.GetCPUInfos()
	if err != nil {
		klog.Fatalf("Fatal error getting CPU topology: %v", err)
	}
	for _, cpu := range cpuInfo {
		cpuIDs = append(cpuIDs, cpu.CpuID)
	}

	allCPUsSet := cpuset.New(cpuIDs...)

	return &PodConfigStore{
		configs:    make(map[types.UID]PodCPUAssignments),
		allCPUs:    allCPUsSet,
		publicCPUs: allCPUsSet.Clone(),
	}
}

// SetContainerState records or updates a container's CPU allocation using a state object.
func (s *PodConfigStore) SetContainerState(podUID types.UID, state *ContainerCPUState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.configs[podUID]; !ok {
		s.configs[podUID] = make(PodCPUAssignments)
	}

	// Use the container name from the state object as the map key.
	s.configs[podUID][state.ContainerName] = state

	if state.Type == CPUTypeGuaranteed {
		// Recalculate public CPUs after any change that could affect guaranteed CPUs.
		s.recalculatePublicCPUs()
	}

	if state.Type == CPUTypeGuaranteed {
		klog.Infof("Set Guaranteed CPUs for PodUID:%v Container:%s guaranteedCPUs:%v", podUID, state.ContainerName, state.guaranteedCPUs.String())
	} else {
		klog.Infof("Set PodUID:%v Container:%s to Shared", podUID, state.ContainerName)
	}
}

// RemoveContainerState removes a container's state from the store.
func (s *PodConfigStore) RemoveContainerState(podUID types.UID, containerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	podAssignments, ok := s.configs[podUID]
	if !ok {
		return
	}

	state, ok := podAssignments[containerName]
	if !ok {
		return
	}

	klog.Infof("Removing container state for %s from PodUID %v", containerName, podUID)
	delete(podAssignments, containerName)

	// If this was the last container for the pod, clean up the pod entry itself.
	if len(podAssignments) == 0 {
		delete(s.configs, podUID)
	}

	// If a guaranteed container was removed, its CPUs must be returned to the public pool.
	if state.Type == CPUTypeGuaranteed {
		s.recalculatePublicCPUs()
		klog.Infof("Recalculated public CPUs after removing guaranteed container.")
	}
}

// TODO(pravk03): Cache this and return in O(1).
func (s *PodConfigStore) GetContainersWithSharedCPUs() []types.UID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sharedCPUContainers := []types.UID{}
	for _, podAssignments := range s.configs {
		for _, state := range podAssignments {
			if state.Type == CPUTypeShared {
				sharedCPUContainers = append(sharedCPUContainers, state.ContainerUID)
			}
		}
	}

	return sharedCPUContainers
}

// IsContainerGuaranteed checks if the container has a guaranteed CPU allocation.
func (s *PodConfigStore) IsContainerGuaranteed(podUID types.UID, containerName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	podAssignments, ok := s.configs[podUID]
	if !ok {
		klog.Warningf("Pod with UID %v not found in PodConfigStore", podUID)
		return false
	}

	containerState, ok := podAssignments[containerName]
	if !ok {
		klog.Warningf("Container %s not found in Pod %v in PodConfigStore", containerName, podUID)
		return false
	}
	return containerState.Type == CPUTypeGuaranteed
}

// IsPodGuaranteed checks if any container in the pod has a guaranteed CPU allocation.
func (s *PodConfigStore) IsPodGuaranteed(podUID types.UID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	podAssignments, ok := s.configs[podUID]
	if !ok {
		klog.Warningf("Pod with UID %v not found in PodConfigStore", podUID)
		return false
	}

	for _, state := range podAssignments {
		if state.Type == CPUTypeGuaranteed {
			return true
		}
	}
	return false
}

// GetPublicCPUs calculates and returns the list of CPUs not reserved by any Guaranteed containers.
// This is the pool available for Shared containers.
func (s *PodConfigStore) GetPublicCPUs() cpuset.CPUSet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.publicCPUs
}

func (s *PodConfigStore) recalculatePublicCPUs() {
	guaranteedCPUs := cpuset.New()
	for _, podAssignments := range s.configs {
		for _, state := range podAssignments {
			if state.Type == CPUTypeGuaranteed {
				guaranteedCPUs = guaranteedCPUs.Union(state.guaranteedCPUs)
			}
		}
	}
	s.publicCPUs = s.allCPUs.Difference(guaranteedCPUs)
}

func (s *PodConfigStore) DeletePodState(podUID types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.configs[podUID]; !ok {
		return
	}
	recalculatePublicCPUs := false
	for _, state := range s.configs[podUID] {
		if state.Type == CPUTypeGuaranteed {
			recalculatePublicCPUs = true
			break
		}
	}
	delete(s.configs, podUID)
	if recalculatePublicCPUs {
		s.recalculatePublicCPUs()
	}
}
