# simple-cpu-dra-driver

**Work In Progress**.

## Overview

The `simple-cpu-dra-driver` is a Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) driver that enables CPU pinning for workloads using DRA framework. This implements the proposal from the [feasability doc](https://docs.google.com/document/d/1Tb_dC60YVCBr7cNYWuVLddUUTMcNoIt3zjd5-8rgug0/edit?tab=t.0#heading=h.iutbebngx80e)

## Requirements 

* Static CPU policy needs to disabled in the cluster. CPU pinning is exclusively managed by the NRI plugin. 
* Containers requesting CPU resources through ResourceClaim should also request CPU through standard resources to ensude kube-scheduler'd view of available CPU is correct. 

## Currently Supported Features
* The NRI plugin pins the CPU's assigned through DRA resource claim. These pinned CPU's are NOT (yet) excluded from other containers.
