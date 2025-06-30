package driver

import (
	"strings"
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

// ClaimCPUAssignments maps a specific claim (by its NamespacedName) to the list of CPUs it provided.
type ClaimCPUAssignments map[types.NamespacedName]cpuset.CPUSet

// ContainerCPUState holds the CPU allocation type and all claim assignments for a container.
type ContainerCPUState struct {
	Type             CPUType
	ClaimAssignments ClaimCPUAssignments
	ContainerName    string
	ContainerUID     types.UID
	guaranteedCPUs   cpuset.CPUSet
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

// SetGuaranteedContainerState records or updates a Guaranteed container's CPU allocation from a specific claim.
func (s *PodConfigStore) SetGuaranteedContainerState(podUID types.UID, containerName string, claimName types.NamespacedName, cpuList []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	claimCPUs, err := cpuset.Parse(strings.Join(cpuList, ","))
	if err != nil {
		return err
	}

	// Ensure pod and container entries exist.
	if _, ok := s.configs[podUID]; !ok {
		s.configs[podUID] = make(PodCPUAssignments)
	}
	podAssignments := s.configs[podUID]

	if _, ok := podAssignments[containerName]; !ok {
		s.configs[podUID][containerName] = &ContainerCPUState{
			Type:             CPUTypeGuaranteed,
			ClaimAssignments: make(ClaimCPUAssignments),
			ContainerName:    containerName,
		}
	}
	podAssignments[containerName].ClaimAssignments[claimName] = claimCPUs
	podAssignments[containerName].guaranteedCPUs = podAssignments[containerName].guaranteedCPUs.Union(claimCPUs)

	s.recalculatePublicCPUs()
	klog.Infof("Set Guaranteed CPUs for PodUID:%v Container:%s Claim:%v CPUs:%v s.configs[podUID][containerName].guaranteedCPUs:%v", podUID, containerName, claimName, claimCPUs.String(), s.configs[podUID][containerName].guaranteedCPUs.String())
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
		Type: CPUTypeShared,
		// No specific claims for shared containers.
		ClaimAssignments: nil,
		ContainerName:    containerName,
		ContainerUID:     containerUID,
	}
	klog.Infof("Set container %s in pod %v to Shared", containerName, podUID)
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
	reservedCPUs := cpuset.New()
	for _, podAssignments := range s.configs {
		for _, state := range podAssignments {
			if state.Type == CPUTypeGuaranteed {
				reservedCPUs = reservedCPUs.Union(state.guaranteedCPUs)
			}
		}
	}
	s.publicCPUs = s.allCPUs.Difference(reservedCPUs)
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

// DeleteClaim removes all CPU assignments associated with a specific claim across all containers.
func (s *PodConfigStore) DeleteClaim(claimNameToDelete types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recalculatePublicCPUs := false
	klog.Infof("Attempting to delete all assignments for claim: %v", claimNameToDelete)
	for podUID, podAssignments := range s.configs {
		for containerName, state := range podAssignments {
			if state.Type == CPUTypeGuaranteed {
				if _, claimExists := state.ClaimAssignments[claimNameToDelete]; claimExists {
					state.guaranteedCPUs = state.guaranteedCPUs.Difference(state.ClaimAssignments[claimNameToDelete])
					delete(state.ClaimAssignments, claimNameToDelete)
					klog.Infof("Removed claim '%v' from container '%s' in pod '%v'", claimNameToDelete, containerName, podUID)
					recalculatePublicCPUs = true
				}
			}
		}
	}
	if recalculatePublicCPUs {
		s.recalculatePublicCPUs()
	}
}
