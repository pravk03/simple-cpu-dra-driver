package driver

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type PodConfig struct {
	RequestCPUIDs map[string][]string
	ClaimRequests map[types.NamespacedName]map[string]struct{}
}

type PodConfigStore struct {
	mu      sync.RWMutex
	configs map[types.UID]PodConfig
}

func NewPodConfigStore() *PodConfigStore {
	return &PodConfigStore{
		configs: make(map[types.UID]PodConfig),
	}
}

func (s *PodConfigStore) SetClaimRequestCPUs(podUID types.UID, claim types.NamespacedName, claimRequest string, cpus []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.configs[podUID]; !ok {
		s.configs[podUID] = PodConfig{}
	}

	podConfig := s.configs[podUID]
	if podConfig.RequestCPUIDs == nil {
		podConfig.RequestCPUIDs = make(map[string][]string)
	}

	podConfig.RequestCPUIDs[claimRequest] = cpus

	if podConfig.ClaimRequests == nil {
		podConfig.ClaimRequests = make(map[types.NamespacedName]map[string]struct{})
	}
	if _, ok := podConfig.ClaimRequests[claim][claimRequest]; !ok {
		podConfig.ClaimRequests[claim] = make(map[string]struct{})
	}
	podConfig.ClaimRequests[claim][claimRequest] = struct{}{}
	s.configs[podUID] = podConfig

	klog.Infof("SetClaimCPUs podUID:%v  claim:%v cpus:%v", podUID, claimRequest, cpus)
	klog.Infof("SetClaimCPUs s.configs:%+v", s.configs)
}

func (s *PodConfigStore) GetClaimRequestCPUs(podUID types.UID, claimRequest string) ([]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	klog.Infof("GetClaimCPUs podUID:%v  claim:%v", podUID, claimRequest)
	klog.Infof("GetClaimCPUs s.configs:%+v", s.configs)
	if podConfig, ok := s.configs[podUID]; ok {
		if claimCpus, ok := podConfig.RequestCPUIDs[claimRequest]; ok {
			return claimCpus, true
		}
	}
	return []string{}, false
}

func (s *PodConfigStore) DeletePod(podUID types.UID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.configs, podUID)
}

func (s *PodConfigStore) DeleteClaimRequest(claim types.NamespacedName) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for podUID, podConfig := range s.configs {
		if podConfig.RequestCPUIDs == nil {
			klog.Infof("DeleteClaim: ClaimCPUIDs map for pod UID %v is nil, nothing to do.", podUID)
			return
		}

		for _, requests := range podConfig.ClaimRequests {
			for request := range requests {
				delete(podConfig.RequestCPUIDs, request)
				klog.Infof("DeleteClaim: deleted claim request '%s' for pod UID %v.", request, podUID)
			}
		}

		s.configs[podUID] = podConfig

		if len(podConfig.RequestCPUIDs) == 0 {
			klog.Infof("DeleteClaim: ClaimCPUIDs map for pod UID %v is now empty. Deleting pod config entry.", podUID)
			delete(s.configs, podUID)
		}
	}
	klog.Infof("SetClaimCPUs s.configs:%+v", s.configs)
}
