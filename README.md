# simple-cpu-dra-driver

> **⚠️ Proof of Concept: This driver is in the very early stages of development.**
>
> This project is a **Proof of Concept (POC)** and is not suitable for production use. It is under active development, and the code should be considered experimental.
>
> * There are currently **no unit or end-to-end tests**.
> * The code may contain **bugs and breaking changes** may be introduced frequently.

## Overview

The `simple-cpu-dra-driver` is a Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) driver that enables CPU pinning for workloads using DRA framework. This project uses a combination of a DRA driver and an NRI (Node Resource Interface) plugin to manage CPU allocations. It ensures that high-priority pods requesting exclusive CPUs through Resource Claim get them, while also managing the remaining CPUs for shared (BestEffort) pods.

This implements the proposal from the [feasability doc](https://docs.google.com/document/d/1Tb_dC60YVCBr7cNYWuVLddUUTMcNoIt3zjd5-8rgug0/edit?tab=t.0#heading=h.iutbebngx80e)

## Requirements 

* Kubernetes Version: v1.33 or higher.
* Feature Gates: The following feature gates must be enabled in your cluster.
    * `DynamicResourceAllocation`
    * `KubeletPodResourcesGet`
    * `DRAResourceClaimDeviceStatus`
    * `KubeletPodResourcesDynamicResources`  
* Kubelet CPUManager: The Kubelet's default CPUManager policy must be set to none, as this driver takes full responsibility for CPU pinning.
* Pod Specs: Pods requesting exclusive CPUs via a ResourceClaim should also include standard CPU requests to ensure the Kubernetes scheduler can make correct placement decisions.

## How it Works 

The driver is deployed as a DaemonSet which contains two core components:
* DRA Kubelet Plugin: This component is responsible for allocating exclusive CPU cores to pods that request them via a ResourceClaim.
* NRI Plugin: This plugin intercepts container creation and update events from the container runtime. It reads the CPU assignments determined by the DRA driver and uses them to pin containers to their designated cores. It also ensures that any containers not requesting exclusive cores are confined to the shared pool of available CPUs.

## Feature Support

### Currently Supported
* Exclusive CPU Allocation: Guaranteed pods that request CPUs via a ResourceClaim are allocated exclusive cores.
* Shared CPU Pool Management: All other containers without a ResourceClaim are confined to a shared pool of CPUs that are not reserved for Guaranteed pods.

### Not Yet Supported
* Management of Pre-existing Pods: The driver does not manage containers that were already running on the node before the driver's daemonset started. This can result in shared pods continuing to run on CPUs that are later allocated to new Guaranteed pods. A restart of these older containers is required to bring them under the driver's management.


## Getting Started

### Installation

* Create a kind cluster 
    * `kind create cluster --config kind.yaml`
* Deploy the driver and all necessary RBAC configurations using the provided manifest
    * `kubectl apply -f install.yaml`

### Example Usage

* Create a DeviceClass: This tells Kubernetes how to find the resources provided by this driver.
    * `kubectl apply -f examples/sample_device_class.yaml`
* Create a ResourceClaim: This requests a specific number of exclusive CPUs from the driver.
    * `kubectl apply -f examples/sample_resource_claim.yaml`
* Create a Pod: Reference the ResourceClaim in your pod spec to receive the allocated CPUs.
    * `kubectl apply -f examples/sample_pod.yaml`

