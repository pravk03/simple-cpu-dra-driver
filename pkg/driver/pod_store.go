package driver

import (
	"sync"

	"github.com/pravk03/topologyutil/pkg/cpuinfo"
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
		cpuIDs = append(cpuIDs, cpu.CpuId)
	}

	allCPUsSet := cpuset.New(cpuIDs...)

	return &PodConfigStore{
		configs:    make(map[types.UID]PodCPUAssignments),
		allCPUs:    allCPUsSet,
		publicCPUs: allCPUsSet.Clone(),
	}
}

// SetGuaranteedContainerState records or updates a Guaranteed container's CPU allocation.
func (s *PodConfigStore) SetGuaranteedContainerState(podUID types.UID, containerName string, containerUID types.UID, guaranteedCPUs cpuset.CPUSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure pod and container entries exist.
	if _, ok := s.configs[podUID]; !ok {
		s.configs[podUID] = make(PodCPUAssignments)
	}
	podAssignments := s.configs[podUID]

	if _, ok := podAssignments[containerName]; !ok {
		s.configs[podUID][containerName] = &ContainerCPUState{
			Type:           CPUTypeGuaranteed,
			ContainerName:  containerName,
			ContainerUID:   containerUID,
			guaranteedCPUs: guaranteedCPUs,
		}
	}

	s.recalculatePublicCPUs()
	klog.Infof("Set Guaranteed CPUs for PodUID:%v Container:%s guaranteedCPUs:%v", podUID, containerName, s.configs[podUID][containerName].guaranteedCPUs.String())
	return nil
}

// SetSharedContainerState marks a container as running on the shared CPU pool.
func (s *PodConfigStore) SetSharedContainerState(podUID types.UID, containerName string, containerUID types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure pod and container entries exist.
	if _, ok := s.configs[podUID]; !ok {
		s.configs[podUID] = make(PodCPUAssignments)
	}
	s.configs[podUID][containerName] = &ContainerCPUState{
		Type:          CPUTypeShared,
		ContainerName: containerName,
		ContainerUID:  containerUID,
	}
	klog.Infof("Set container %s in PodUID %v to Shared", containerName, podUID)
}

// GetGuaranteedCPUs returns the complete set of CPUs for a guaranteed container by aggregating CPUs from all their claims.
func (s *PodConfigStore) GetGuaranteedCPUs(podUID types.UID, containerName string) (cpuset.CPUSet, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cpus := cpuset.New()
	podAssignments, podFound := s.configs[podUID]
	if !podFound {
		return cpus, false
	}

	state, containerFound := podAssignments[containerName]
	if !containerFound {
		return cpus, false
	}

	if state.Type == CPUTypeShared {
		return cpus, false
	}

	klog.Infof("GetGuaranteedCPUs state.guaranteedCPUs:%s", state.guaranteedCPUs.String())
	// For Guaranteed containers, aggregate CPUs from all associated claims.
	return state.guaranteedCPUs, true
}

// TODO(pravk03): Cache this and return in O(1).
func (s *PodConfigStore) GetContainersWithSharedCPUs() []types.UID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sharedCPUContainers := []types.UID{}
	for _, podAssignments := range s.configs {
		for _, state := range podAssignments {
			if state.Type == CPUTypeShared {
				klog.Infof("Found shared container %+v", state)
				sharedCPUContainers = append(sharedCPUContainers, state.ContainerUID)
			}
		}
	}
	return sharedCPUContainers
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

func (s *PodConfigStore) DeletePod(podUID types.UID) {
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
	if recalculatePublicCPUs {
		s.recalculatePublicCPUs()
	}
	delete(s.configs, podUID)
}
