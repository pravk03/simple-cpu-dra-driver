package driver

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// ClaimCPUAssignments maps a specific claim (by its NamespacedName) to the list of CPUs it provided.
type ClaimCPUAssignments map[types.NamespacedName][]string

// ContianerAssignments maps a container's name to all of its claim-based CPU assignments.
type ContianerAssignments map[string]ClaimCPUAssignments

// PodConfigStore now maps a Pod's UID directly to its container-level CPU assignments.
// The complexity of claims and requests is now handled before calling the store's methods.
type PodConfigStore struct {
	mu      sync.RWMutex
	configs map[types.UID]ContianerAssignments
}

func NewPodConfigStore() *PodConfigStore {
	return &PodConfigStore{
		configs: make(map[types.UID]ContianerAssignments),
	}
}

// SetContainerCPUs directly associates a list of CPUs with a specific container in a pod.
func (s *PodConfigStore) SetContainerCPUs(podUID types.UID, containerName string, claimName types.NamespacedName, cpus []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get or create the assignments for this pod.
	podAssignments, ok := s.configs[podUID]
	if !ok {
		podAssignments = make(ContianerAssignments)
		s.configs[podUID] = podAssignments
	}

	// Get or create the assignments for this container.
	containerAssignments, ok := podAssignments[containerName]
	if !ok {
		containerAssignments = make(ClaimCPUAssignments)
		podAssignments[containerName] = containerAssignments
	}

	// Finally, set the CPUs for the specific claim for this container.
	containerAssignments[claimName] = cpus
	klog.Infof("Set CPUs for PodUID:%v Container:%v CPUs:%v", podUID, containerName, cpus)
	klog.V(5).Infof("PodConfigStore state: %+v", s.configs)
}

// GetContainerCPUs directly retrieves the CPUs for a specific container in a pod.
func (s *PodConfigStore) GetContainerCPUs(podUID types.UID, containerName string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cpus := []string{}
	if claimAssignments, ok := s.configs[podUID]; ok {
		if claims, ok := claimAssignments[containerName]; ok {
			for claimName, claimCPUIDs := range claims {
				cpus = append(cpus, claimCPUIDs...)
				klog.Infof("Found Claim:%v with CPUs:%v", claimName, claimCPUIDs)
			}
		} else {
			klog.Infof("No claims found for PodUID:%v container:%s", podUID, containerName)
			return nil, false
		}
	} else {
		klog.Infof("PodUID:%v not found", podUID)
		return nil, false
	}
	klog.Infof("Returning cpus:%v for podId:%v container:%s", cpus, podUID, containerName)
	return cpus, true
}

func (s *PodConfigStore) DeletePod(podUID types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.configs, podUID)
}

// DeleteClaim removes all CPU assignments for a specific claim from all pods in the store.
func (s *PodConfigStore) DeleteClaim(claimName types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()

	klog.Infof("Attempting to delete all assignments for claim: %v", claimName)
	emptyPods := []types.UID{}
	for podUID, containerAssignments := range s.configs {
		for containerName, claimAssignments := range containerAssignments {
			if _, claimExists := claimAssignments[claimName]; claimExists {
				delete(claimAssignments, claimName)
				klog.Infof("Removed claim '%v' from container '%s' in pod '%v'", claimName, containerName, podUID)
			}

			if len(claimAssignments) == 0 {
				delete(containerAssignments, containerName)
			}
		}

		if len(containerAssignments) == 0 {
			emptyPods = append(emptyPods, podUID)
		}
	}
	// Finally, remove the entries for any pods that are now completely empty.
	// This is done after the loop to avoid modifying the map while iterating.
	for _, podUID := range emptyPods {
		klog.Infof("Pod %v has no more claim assignments, removing its entry from the store.", podUID)
		delete(s.configs, podUID)
	}
}
