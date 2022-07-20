# knest: Kubernetes-in-Kubernetes Made Simple

[![build](https://github.com/smartxworks/knest/actions/workflows/build.yml/badge.svg)](https://github.com/smartxworks/knest/actions/workflows/build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/smartxworks/knest)](https://goreportcard.com/report/github.com/smartxworks/knest)

## Installation

### Prerequisites

- Your host Kubernetes cluster should meet [Virtink's requirements](https://github.com/smartxworks/virtink#requirements)
- Your local environment should have [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) and [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl) installed

### Install knest

Binaries for Linux, Windows and Mac are available in the [release](https://github.com/smartxworks/knest/releases) page.

## Getting Started

### Create a Nested Kubernetes Cluster

You can create a nested Kubernetes cluster simply by typing:

```bash
knest create quickstart
```

knest would automatically install any missing components (Cluster API providers and Virtink) on the host cluster, create certain number of Virtink VMs, and form them into a new Kubernetes cluster. When the control plane of the new cluster is initialized, a corresponding kubeconfig file would be saved in the canonical kubeconfig directory (`$HOME/.kube/`) for you to further access and control the created cluster.

> ⚠️ Please be awared that the pod subnet and the service subnet of your nested cluster should not overlap with host cluster's pod subnet, service subnet or physical subnet. Use `--pod-network-cidr` and `--service-cidr` flags to configure nested cluster's pod subnet and service subnet respectively when necessary.

### Scale the Nested Kubernetes Cluster

You can scale your nested cluster easily as follows:

```bash
knest scale quickstart 3:3
```

The last argument is in the format of `CONTROL_PLANE_MACHINE_COUNT:WORKER_MACHINE_COUNT`. Leave `CONTROL_PLANE_MACHINE_COUNT` or `WORKER_MACHINE_COUNT` blank if you don't want to change it.

### Delete the Nested Kubernetes Cluster

You can delete your nested cluster as follows:

```bash
knest delete quickstart
```

Please be noted that this operation would delete all VMs and data of the nested cluster.

## Demo Recording

[![asciicast](https://asciinema.org/a/509497.svg)](https://asciinema.org/a/509497)

## Known Issues

- If no CNI plugin is installed in the nested cluster, worker nodes would get re-created about every 10 minutes. This is currently an expected behaviour due to our MachineHealthCheck settings. Once a valid CNI plugin is installed and running, this problem would disappear.
- Currently [Calico](https://projectcalico.docs.tigera.io/getting-started/kubernetes/quickstart) is the only recommended CNI plugin for nested clusters, due to limited kernel modules was included in the image. Supports for more CNI plugins like Cilium is on the way.

## License

This project is distributed under the [Apache License, Version 2.0](LICENSE).
