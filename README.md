# simple-cpu-dra-driver

## Overview

The `simple-cpu-dra-driver` is a Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) driver that enables CPU pinning for workloads using DRA framework. It exposes individual logical CPUs on a node as discoverable resources, allowing Kubernetes Pods to request and be pinned to specific CPUs.

## How It Works

This driver operates as a bridge between Kubernetes' DRA framework and the `containerd` runtime's NRI.

1.  **Resource Discovery & Publication:**
    * The DRA drive scans the node's CPU topology and identifies each logical CPU (along with its Core ID, CPU ID, and NUMA Node) and publishes it to the Kubernetes API server as a DRA `ResourceSlice`.

2.  **Container CPU Pinning (NRI Hook):**
    * During the container creation lifecycle (intercepted by the `containerd` NRI hooks), it  applies low-level CPU adjustments (pinning) to the container using CRI (Container Runtime Interface) mechanisms, ensuring the container runs exclusively on the requested logical CPUs.

## Usage

1.  **Prerequisites:**
    * A Kubernetes cluster with the Dynamic Resource Allocation (DRA) feature enabled  (`kind.yaml` creates a kind cluster with DRA enabled).
    * `containerd` runtime configured on your worker nodes.
    * Nodes with accessible CPU topology information (e.g., `/proc/cpuinfo`, `/sys/devices/system/cpu`).
2.  **Driver Deployment:** Deploy the `simple-cpu-dra-driver` as a DaemonSet across your worker nodes (`install.yaml`).
3.  **Define ResourceClass and ResourceClaim:** A cluster administrator would define a Kubernetes `ResourceClass` and users would then create `ResourceClaim` objects in their namespaces, specifying their request for CPU resources. `examples\` directory has sample objects.