// SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
// SPDX-License-Identifier: Apache-2.0

package cpuinfo

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/utils/cpuset"
)

type CPUInfo struct {
	// CpuId is the enumerated CPU ID
	CpuId int `json:"cpuId"`

	// CoreId is the logical core ID, unique within each SocketId
	CoreId int `json:"coreId"`

	// SocketId is the physical socket ID
	SocketId int `json:"socketId"`

	// Numa Node is the NUMA node ID, unique within each SocketId
	NumaNode int `json:"numaNode"`

	// NUMA Node Affinity Mask
	NumaNodeAffinityMask string `json:"numaNodeAffinityMask"`

	// Core Type (e-core or p-core)
	CoreType string `json:"coreType"`

	// L3 cache ID
	L3CacheId int64 `json:"l3CacheId"`
}

func GetCPUInfos() ([]CPUInfo, error) {

	filename := HostProc("cpuinfo")
	lines, err := ReadLines(filename)
	if err != nil {
		return []CPUInfo{}, err
	}

	eCoreFilename := HostSys("devices/cpu_atom/cpus")
	eCoreLines, err := ReadLines(eCoreFilename)
	var eCoreCpus cpuset.CPUSet
	if err == nil {
		eCoreCpus, err = cpuset.Parse(eCoreLines[0])
		if err != nil {
			return []CPUInfo{}, err
		}
	}

	cpuInfos := []CPUInfo{}
	var cpuInfoLines []string
	for _, line := range lines {
		// `/proc/cpuinfo` uses empty lines to denote a new CPU block of data.
		if strings.TrimSpace(line) == "" {
			// Parse and reset CPU lines.
			cpuInfo := parseCPUInfo(eCoreCpus, cpuInfoLines...)
			if cpuInfo != nil {
				cpuInfos = append(cpuInfos, *cpuInfo)
			}
			cpuInfoLines = []string{}
		} else {
			// Gather CPU info lines for later processing.
			cpuInfoLines = append(cpuInfoLines, line)
		}
	}
	if err := populateL3CacheIDs(cpuInfos); err != nil {
		log.Printf("Warning: failed to populate L3 cache IDs: %v", err)
	}
	if err := populateNumaInfos(cpuInfos); err != nil {
		log.Printf("Warning: failed to populate NUMA info: %v", err)
	}

	return cpuInfos, nil
}

func parseCPUInfo(eCoreCpus cpuset.CPUSet, lines ...string) *CPUInfo {
	cpuInfo := &CPUInfo{
		CpuId:                -1,
		SocketId:             -1,
		CoreId:               -1,
		NumaNode:             -1,
		NumaNodeAffinityMask: "",
		L3CacheId:            -1,
	}

	if len(lines) == 0 {
		return nil
	}

	for _, line := range lines {
		// Within each CPU block of data, each line uses ':' to separate the
		// key-value pair (with whitespace padding).
		fields := strings.Split(line, ":")
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSpace(fields[0])
		value := strings.TrimSpace(fields[1])

		switch key {
		case "processor":
			cpuInfo.CpuId = parseInt(value)
		case "physical id":
			cpuInfo.SocketId = parseInt(value)
		case "core id":
			cpuInfo.CoreId = parseInt(value)
		}
	}

	cpuInfo.CoreType = "p-core"
	if eCoreCpus.Contains(cpuInfo.CpuId) {
		cpuInfo.CoreType = "e-core"
	}

	if cpuInfo.CpuId < 0 || cpuInfo.SocketId < 0 || cpuInfo.CoreId < 0 {
		return nil
	}

	return cpuInfo
}

func populateL3CacheIDs(cpuInfos []CPUInfo) error {
	for i := range cpuInfos {
		if cpuInfos[i].L3CacheId != -1 {
			continue
		}

		cachePath := HostSys(fmt.Sprintf("devices/system/cpu/cpu%d/cache", cpuInfos[i].CpuId))
		entries, err := os.ReadDir(cachePath)
		if err != nil {
			return fmt.Errorf("could not read cache dir %s: %w", cachePath, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "index") {
				continue
			}

			levelPath := filepath.Join(cachePath, entry.Name(), "level")
			levelStr, err := ReadFile(levelPath)
			if err != nil {
				continue
			}

			if strings.TrimSpace(levelStr) == "3" {
				l3CacheDir := filepath.Join(cachePath, entry.Name())
				cacheIdPath := filepath.Join(l3CacheDir, "id")
				idStr, err := ReadFile(cacheIdPath)
				if err != nil {
					return fmt.Errorf("could not read L3 cache id from %s: %w", cacheIdPath, err)
				}
				id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
				if err != nil {
					return fmt.Errorf("could not parse L3 cache id '%s': %w", idStr, err)
				}

				sharedCPUListPath := filepath.Join(l3CacheDir, "shared_cpu_list")
				sharedCPUListStr, err := ReadFile(sharedCPUListPath)
				if err != nil {
					return fmt.Errorf("could not read shared_cpu_list from %s: %w", sharedCPUListPath, err)
				}

				sharedCPUSet, err := cpuset.Parse(sharedCPUListStr)
				if err != nil {
					return fmt.Errorf("could not parse shared_cpu_list '%s': %w", sharedCPUListStr, err)
				}

				// Update the L3Cache ID for all the cpus with the same cache.
				for j := range cpuInfos {
					if sharedCPUSet.Contains(cpuInfos[j].CpuId) {
						cpuInfos[j].L3CacheId = id
					}
				}
				break
			}
		}
	}
	return nil
}

func populateNumaInfos(cpuInfos []CPUInfo) error {
	for i := range cpuInfos {
		if cpuInfos[i].NumaNode != -1 {
			continue
		}

		nodePath := HostSys(fmt.Sprintf("devices/system/cpu/cpu%d", cpuInfos[i].CpuId))
		files, err := os.ReadDir(nodePath)
		if err != nil {
			return err
		}

		for _, file := range files {
			if strings.HasPrefix(file.Name(), "node") {
				nodeId, err := strconv.ParseInt(strings.TrimPrefix(file.Name(), "node"), 10, 64)
				if err != nil {
					continue
				}

				mask, err := ReadLines(HostSys(fmt.Sprintf("devices/system/node/node%d/cpumap", nodeId)))
				if err != nil {
					return err
				}

				numaCPUSet, err := cpuset.Parse(mask[0])
				if err != nil {
					return fmt.Errorf("could not parse cpumap '%s': %w", mask[0], err)
				}

				for j := range cpuInfos {
					if numaCPUSet.Contains(cpuInfos[j].CpuId) {
						cpuInfos[j].NumaNode = int(nodeId)
						cpuInfos[j].NumaNodeAffinityMask = formatAffinityMask(mask[0])
					}
				}
				break
			}
		}
	}
	return nil
}

func formatAffinityMask(mask string) string {
	newMask := strings.ReplaceAll(mask, ",", "")
	newMask = strings.TrimSpace(newMask)
	return "0x" + newMask
}

func parseInt(str string) int {
	val, err := strconv.Atoi(str)
	if err != nil {
		panic(err)
	}
	return val
}

// ReadFile reads contents from a file.
func ReadFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ReadLines reads contents from a file and splits them by new lines.
func ReadLines(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")

	return lines, nil
}

func HostRoot(combineWith ...string) string {
	return GetEnv("HOST_ROOT", "/", combineWith...)
}

func HostProc(combineWith ...string) string {
	return HostRoot(combinePath("proc", combineWith...))
}

func HostSys(combineWith ...string) string {
	return HostRoot(combinePath("sys", combineWith...))
}

// GetEnv retrieves the environment variable key, or uses the default value.
func GetEnv(key string, otherwise string, combineWith ...string) string {
	value := os.Getenv(key)
	if value == "" {
		value = otherwise
	}

	return combinePath(value, combineWith...)
}

func combinePath(value string, combineWith ...string) string {
	switch len(combineWith) {
	case 0:
		return value
	case 1:
		return filepath.Join(value, combineWith[0])
	default:
		all := make([]string, len(combineWith)+1)
		all[0] = value
		copy(all[1:], combineWith)
		return filepath.Join(all...)
	}
}
