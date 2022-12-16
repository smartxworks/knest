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

### Create a Persistent Nested Kubernetes Cluster

A persistent nested Kubernetes cluster is a nested cluster that each of its nodes will have a persistent rootfs and a static IP address. To create a persistent nested Kubernetes cluster, the following prerequisites should be met:

- Your host cluster should have defined a default StorageClass, knest will use it to create PVC for each nested cluster node.
- Your host cluster's CNI plugin should support static IP assignment feature for pod, knest will use it to assign static IP for each nested cluster node. Currently knest has built-in static IP assignment support for Calico and Kube-OVN.

Below is an example to create a persistent nested cluster:

```bash
knest create quickstart-persistent --persistent --machine-addresses=172.22.127.100-172.22.127.110 --host-cluster-cni=calico
```

For other CNI plugins, you can download the default [cluster template](https://github.com/smartxworks/cluster-api-provider-virtink/tree/main/templates), modify it accordingly, and specify it using the `--cluster-template` flag.

### Scale the Nested Kubernetes Cluster

You can scale your nested cluster easily as follows:

```bash
knest scale quickstart --control-plane-machine-count=3 --worker-machine-count=2
```

### Delete the Nested Kubernetes Cluster

You can delete your nested cluster as follows:

```bash
knest delete quickstart
```

Please be noted that this operation would delete all VMs and data of the nested cluster.

## Demo Recording

[![asciicast](https://asciinema.org/a/509497.svg)](https://asciinema.org/a/509497)

## Known Issues

- Sometimes you may encounter an error with a message like `... rate limit for github api has been reached. Please wait one hour or get a personal API token and assign it to the GITHUB_TOKEN environment variable`, this is a known issue with clusterctl. To work around this, create a personal access token on your GitHub settings page and assign it to the `GITHUB_TOKEN` environment variable.
- If no CNI plugin is installed in the nested cluster, worker nodes would get re-created about every 5 minutes. This is currently an expected behaviour due to our MachineHealthCheck settings. Once a valid CNI plugin is installed and running, this problem would disappear.
- Currently [Calico](https://projectcalico.docs.tigera.io/getting-started/kubernetes/quickstart) and [Cilium](https://docs.cilium.io/en/stable/gettingstarted/#getting-started-guides) are the only two recommended CNI plugins for nested clusters, due to limited kernel modules was included in the image. Support for more CNI plugins is on the way. And overlay network is required for nested cluster CNI, the supports for CNI and encapsulation mode are as follows:

  | CNI    | Encapsulation Mode | Encryption |
  | ------ | ------------------ | ---------- |
  | Calico | IPIP               | false      |
  | Calico | VXLAN(4789/UDP)    | false      |
  | Cilium | VXLAN(8472/UDP)    | false      |
  | Cilium | Geneve(6081/UDP)   | false      |

- The underlying network and firewalls must allow encapsulated packets. For example, if host cluster uses Calico as CNI plugin, the IPIP and VXLAN(4789/UDP) encapsulated packets from nested cluster will be `DROP` by default. See [Configuring Felix](https://projectcalico.docs.tigera.io/reference/felix/configuration) to allow IPIP and VXLAN(4789/UDP) packets from workloads.

## License

This project is distributed under the [Apache License, Version 2.0](LICENSE).
