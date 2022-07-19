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

## License

This project is distributed under the [Apache License, Version 2.0](LICENSE).
