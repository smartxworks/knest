package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	VirtinkVersion          = "v0.13.0"
	VirtinkProviderVersion  = "v0.6.0"
	IPAddressManagerVersion = "v1.2.1"
	CDIVersion              = "v1.55.2"
)

var version string

type Version struct {
	Knest                     string `json:"knest"`
	Virtink                   string `json:"virtink"`
	ClusterAPIProviderVirtink string `json:"cluster-api-provider-virtink"`
}

//go:embed templates/*
var templatesFS embed.FS

func main() {
	var (
		targetNamespace                = "default"
		kubernetesVersion              = "1.24.0"
		controlPlaneMachineCount       = 1
		workerMachineCount             = 1
		newControlPlaneMachineCount    = -1
		newWorkerMachineCount          = -1
		podNetworkCIDR                 = "192.168.0.0/16"
		serviceCIDR                    = "10.96.0.0/12"
		controlPlaneMachineCPUCores    = 2
		controlPlaneMachineMemorySize  = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		controlPlaneMachineKernelImage = ""
		controlPlaneMachineRootfsImage = ""
		controlPlaneMachineRootfsSize  = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		workerMachineCPUCores          = 2
		workerMachineMemorySize        = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		workerMachineKernelImage       = ""
		workerMachineRootfsImage       = ""
		workerMachineRootfsSize        = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		persistent                     = false
		machineAddresses               []string
		hostClusterCNI                 string
		from                           string
	)

	cmdCreate := &cobra.Command{
		Use:   "create CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Create a nested Kubernetes cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setupClusterctlConfig(); err != nil {
				return fmt.Errorf("setup clusterctl config: %s", err)
			}

			capchCRDOutput, err := getCommandOutput(exec.Command("kubectl", "get", "crd", "virtinkclusters.infrastructure.cluster.x-k8s.io", "--ignore-not-found"))
			if err != nil {
				return fmt.Errorf("get Cluster API CRDs: %s", err)
			}
			if len(capchCRDOutput) == 0 {
				fmt.Println("Installing Cluster API providers")
				if err := runCommand(exec.Command("clusterctl", "init", "--infrastructure", fmt.Sprintf("virtink:%s", VirtinkProviderVersion), "--wait-providers")); err != nil {
					return fmt.Errorf("install Cluster API providers: %s", err)
				}
			}

			virtinkCRDOutput, err := getCommandOutput(exec.Command("kubectl", "get", "crd", "virtualmachines.virt.virtink.smartx.com", "--ignore-not-found"))
			if err != nil {
				return fmt.Errorf("get Virtink CRDs: %s", err)
			}
			if len(virtinkCRDOutput) == 0 {
				fmt.Println("Installing Virtink")
				if err := runCommand(exec.Command("kubectl", "apply", "-f", fmt.Sprintf("https://github.com/smartxworks/virtink/releases/download/%s/virtink.yaml", VirtinkVersion))); err != nil {
					return fmt.Errorf("install Virtink: %s", err)
				}

				fmt.Println("Waiting for Virtink to be available...")
				if err := runCommand(exec.Command("kubectl", "wait", "-n", "virtink-system", "deployment", "virt-controller", "--for", "condition=Available", "--timeout", "-1s")); err != nil {
					return fmt.Errorf("wait for Virtink to be available: %s", err)
				}
			}

			cdiCRDOutput, err := getCommandOutput(exec.Command("kubectl", "get", "crd", "datavolumes.cdi.kubevirt.io", "--ignore-not-found"))
			if err != nil {
				return fmt.Errorf("get CDI CRDs: %s", err)
			}
			if len(cdiCRDOutput) == 0 {
				fmt.Println("Installing CDI")
				if err := runCommand(exec.Command("kubectl", "apply", "-f", fmt.Sprintf("https://github.com/kubevirt/containerized-data-importer/releases/download/%s/cdi-operator.yaml", CDIVersion))); err != nil {
					return fmt.Errorf("install CDI operator: %s", err)
				}
				if err := runCommand(exec.Command("kubectl", "apply", "-f", fmt.Sprintf("https://github.com/kubevirt/containerized-data-importer/releases/download/%s/cdi-cr.yaml", CDIVersion))); err != nil {
					return fmt.Errorf("install CDI: %s", err)
				}

				fmt.Println("Waiting for CDI to be available...")
				if err := runCommand(exec.Command("kubectl", "wait", "cdi", "cdi", "--for", "condition=Available", "--timeout", "-1s")); err != nil {
					return fmt.Errorf("wait for CDI to be available: %s", err)
				}
			}

			ipAddressManagerCRDOutput, err := getCommandOutput(exec.Command("kubectl", "get", "crd", "ippools.ipam.metal3.io", "--ignore-not-found"))
			if err != nil {
				return fmt.Errorf("get ip-address-manager CRDs: %s", err)
			}
			if len(ipAddressManagerCRDOutput) == 0 {
				fmt.Println("Installing ip-address-manager")
				if err := runCommand(exec.Command("kubectl", "create", "namespace", "capm3-system")); err != nil {
					return fmt.Errorf("create ip-address-manager namespace: %s", err)
				}
				if err := runCommand(exec.Command("kubectl", "apply", "-f", fmt.Sprintf("https://github.com/metal3-io/ip-address-manager/releases/download/%s/ipam-components.yaml", IPAddressManagerVersion))); err != nil {
					return fmt.Errorf("install ip-address-manager: %s", err)
				}

				fmt.Println("Waiting for ip-address-manager to be available...")
				if err := runCommand(exec.Command("kubectl", "wait", "-n", "capm3-system", "deployment", "ipam-controller-manager", "--for", "condition=Available", "--timeout", "-1s")); err != nil {
					return fmt.Errorf("wait for ip-address-manager to be available: %s", err)
				}
			}

			targetNamespaceOutput, err := getCommandOutput(exec.Command("kubectl", "get", "namespace", targetNamespace, "--ignore-not-found"))
			if err != nil {
				return fmt.Errorf("get target namespace: %s", err)
			}
			if len(targetNamespaceOutput) == 0 {
				if err := runCommand(exec.Command("kubectl", "create", "namespace", targetNamespace)); err != nil {
					return fmt.Errorf("create target namespace: %s", err)
				}
			}
			generateCmd := exec.Command("clusterctl", "generate", "cluster", args[0],
				"--target-namespace", targetNamespace,
				"--kubernetes-version", kubernetesVersion,
				"--control-plane-machine-count", strconv.Itoa(controlPlaneMachineCount),
				"--worker-machine-count", strconv.Itoa(workerMachineCount))
			generateCmd.Env = os.Environ()
			generateCmd.Env = append(generateCmd.Env,
				fmt.Sprintf("POD_NETWORK_CIDR=%s", podNetworkCIDR),
				fmt.Sprintf("SERVICE_CIDR=%s", serviceCIDR),
				"VIRTINK_CONTROL_PLANE_SERVICE_TYPE=NodePort",
				fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_CPU_CORES=%d", controlPlaneMachineCPUCores),
				fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_MEMORY_SIZE=%s", controlPlaneMachineMemorySize.String()),
				fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_KERNEL_IMAGE=%s", controlPlaneMachineKernelImage),
				fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_ROOTFS_IMAGE=%s", controlPlaneMachineRootfsImage),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_SIZE=%s", workerMachineRootfsSize.String()),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_CPU_CORES=%d", workerMachineCPUCores),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_MEMORY_SIZE=%s", workerMachineMemorySize.String()),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_KERNEL_IMAGE=%s", workerMachineKernelImage),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_IMAGE=%s", workerMachineRootfsImage),
				fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_SIZE=%s", workerMachineRootfsSize.String()))

			if from != "" {
				generateCmd.Args = append(generateCmd.Args, "--from", from)
			} else {
				generateCmd.Args = append(generateCmd.Args, "--infrastructure", fmt.Sprintf("virtink:%s", VirtinkProviderVersion))
				if persistent {
					generateCmd.Args = append(generateCmd.Args, "--flavor", "cdi-internal")
				} else {
					generateCmd.Args = append(generateCmd.Args, "--flavor", "internal")
				}
			}

			clusterTemplatePatches := map[string][]byte{}
			if persistent {
				if controlPlaneMachineRootfsImage == "" {
					controlPlaneMachineRootfsImage = "smartxworks/capch-rootfs-cdi-1.24.0"
				}
				if workerMachineRootfsImage == "" {
					workerMachineRootfsImage = "smartxworks/capch-rootfs-cdi-1.24.0"
				}

				generateCmd.Env = append(generateCmd.Env,
					fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_ROOTFS_CDI_IMAGE=%s", controlPlaneMachineRootfsImage),
					fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_CDI_IMAGE=%s", workerMachineRootfsImage),
					fmt.Sprintf("VIRTINK_IP_POOL_NAME=%s", args[0]),
				)

				if err := runCommand(exec.Command("kubectl", "delete", "ippool.ipam.metal3.io", args[0], "--namespace", targetNamespace, "--wait", "--ignore-not-found")); err != nil {
					return fmt.Errorf("delete IPPool: %s", err)
				}

				type ipPoolTemplateDataPool struct {
					Start  string
					End    string
					Subnet string
				}

				ipPoolTemplateData := struct {
					Name      string
					Namespace string
					Pools     []ipPoolTemplateDataPool
				}{
					Name:      args[0],
					Namespace: targetNamespace,
				}

				for _, addr := range machineAddresses {
					if strings.Contains(addr, "-") {
						items := strings.Split(addr, "-")
						if len(items) != 2 {
							return fmt.Errorf("invalid machine address: %s", addr)
						}
						ipPoolTemplateData.Pools = append(ipPoolTemplateData.Pools, ipPoolTemplateDataPool{
							Start: items[0],
							End:   items[1],
						})
					} else {
						ipPoolTemplateData.Pools = append(ipPoolTemplateData.Pools, ipPoolTemplateDataPool{
							Subnet: addr,
						})
					}
				}

				ipPoolDataBuf := &bytes.Buffer{}
				if err := template.Must(template.New("ippool.yaml").ParseFS(templatesFS, "templates/ippool.yaml")).Execute(ipPoolDataBuf, ipPoolTemplateData); err != nil {
					return err
				}

				createIPPoolCmd := exec.Command("kubectl", "apply", "-f", "-")
				createIPPoolCmd.Stdin = ipPoolDataBuf
				if err := runCommand(createIPPoolCmd); err != nil {
					return fmt.Errorf("create IPPool: %s", err)
				}

				if hostClusterCNI != "" {
					var patchFileName string
					switch hostClusterCNI {
					case "calico":
						patchFileName = filepath.Join("templates", "calico-static-ip-and-mac-patches.yaml")
					case "kube-ovn":
						patchFileName = filepath.Join("templates", "kube-ovn-static-ip-and-mac-patches.yaml")
					default:
						return fmt.Errorf("unsupported host cluster CNI: %s", hostClusterCNI)
					}

					patchBytes, err := templatesFS.ReadFile(patchFileName)
					if err != nil {
						return err
					}
					clusterTemplatePatches[patchFileName] = patchBytes
				}
			} else {
				if controlPlaneMachineKernelImage == "" {
					controlPlaneMachineKernelImage = "smartxworks/capch-kernel-5.15.12"
				}
				if controlPlaneMachineRootfsImage == "" {
					controlPlaneMachineRootfsImage = "smartxworks/capch-rootfs-1.24.0"
				}
				if workerMachineKernelImage == "" {
					workerMachineKernelImage = "smartxworks/capch-kernel-5.15.12"
				}
				if workerMachineRootfsImage == "" {
					workerMachineRootfsImage = "smartxworks/capch-rootfs-1.24.0"
				}

				generateCmd.Env = append(generateCmd.Env,
					fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_ROOTFS_IMAGE=%s", controlPlaneMachineRootfsImage),
					fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_IMAGE=%s", workerMachineRootfsImage))
			}

			kustomizeWorkDir, err := os.MkdirTemp("", "knest")
			if err != nil {
				return err
			}

			clusterTemplateFilePath := filepath.Join(kustomizeWorkDir, "cluster-template.yaml")
			clusterTemplateFile, err := os.Create(clusterTemplateFilePath)
			if err != nil {
				return err
			}
			generateCmd.Stdout = clusterTemplateFile
			if err := runCommand(generateCmd); err != nil {
				return fmt.Errorf("clusterctl generate cluster template: %s", err)
			}
			clusterTemplateFile.Close()

			for patchFile, patchBytes := range clusterTemplatePatches {
				kustomizationFilePath := filepath.Join(kustomizeWorkDir, "kustomization.yaml")
				if err := os.WriteFile(kustomizationFilePath, patchBytes, 0644); err != nil {
					return err
				}

				kustomizeCmd := exec.Command("kubectl", "kustomize", kustomizeWorkDir, "--output", clusterTemplateFilePath)
				if err := runCommand(kustomizeCmd); err != nil {
					return fmt.Errorf("kustomize cluster template for %s: %s", patchFile, err)
				}
			}

			fmt.Printf("starting create cluster by %s\n", clusterTemplateFilePath)
			applyCmd := exec.Command("kubectl", "apply", "-f", clusterTemplateFilePath)
			if err := runCommand(applyCmd); err != nil {
				return fmt.Errorf("create cluster resources: %s", err)
			}

			fmt.Println("Waiting for control plane to be initialized...")
			if err := runCommand(exec.Command("kubectl", "wait", "clusters.cluster.x-k8s.io", args[0], "--namespace", targetNamespace, "--for", "condition=ControlPlaneInitialized", "--timeout", "-1s")); err != nil {
				return fmt.Errorf("wait for control plane to be initialized: %s", err)
			}

			// TODO: LB support
			nodePort, err := getCommandOutput(exec.Command("kubectl", "get", "service", args[0], "--namespace", targetNamespace, "-o", "jsonpath={.spec.ports[0].nodePort}"))
			if err != nil {
				return fmt.Errorf("get node port: %s", err)
			}
			clusterIP, err := getCommandOutput(exec.Command("kubectl", "get", "service", args[0], "--namespace", targetNamespace, "-o", "jsonpath={.spec.clusterIP}"))
			if err != nil {
				return fmt.Errorf("get cluster IP: %s", err)
			}

			encodedKubeconfigData, err := getCommandOutput(exec.Command("kubectl", "get", "secret", fmt.Sprintf("%s-kubeconfig", args[0]), "--namespace", targetNamespace, "-o", "jsonpath={.data.value}"))
			if err != nil {
				return fmt.Errorf("get kubeconfig: %s", err)
			}
			kubeconfigData, err := base64.StdEncoding.DecodeString(encodedKubeconfigData)
			if err != nil {
				return fmt.Errorf("decode kubeconfig: %s", err)
			}

			kubeconfigFilePath := filepath.Join(homedir.HomeDir(), ".kube", fmt.Sprintf("knest.%s.%s.kubeconfig", targetNamespace, args[0]))
			if err := os.WriteFile(kubeconfigFilePath, kubeconfigData, 0644); err != nil {
				return fmt.Errorf("save kubeconfig: %s", err)
			}

			infraKubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
			if err != nil {
				return fmt.Errorf("load infra kubeconfig: %s", err)
			}
			infraHost, err := url.Parse(infraKubeconfig.Host)
			if err != nil {
				return fmt.Errorf("parse infra host: %s", err)
			}

			if err := runCommand(exec.Command("kubectl", "config", "--kubeconfig", kubeconfigFilePath, "set-cluster", args[0],
				"--server", fmt.Sprintf("%s://%s:%s", infraHost.Scheme, infraHost.Hostname(), nodePort),
				"--tls-server-name", clusterIP)); err != nil {
				return fmt.Errorf("update kubeconfig: %s", err)
			}

			fmt.Printf("Your cluster %q is now accessible with the kubeconfig file %q\n", args[0], kubeconfigFilePath)
			return nil
		},
	}

	cmdCreate.PersistentFlags().StringVar(&kubernetesVersion, "kubernetes-version", kubernetesVersion, "The Kubernetes version to use for the nested cluster.")
	cmdCreate.PersistentFlags().IntVar(&controlPlaneMachineCount, "control-plane-machine-count", controlPlaneMachineCount, "The number of control plane machines for the nested cluster.")
	cmdCreate.PersistentFlags().IntVar(&workerMachineCount, "worker-machine-count", workerMachineCount, "The number of worker machines for the nested cluster.")
	cmdCreate.PersistentFlags().StringVar(&podNetworkCIDR, "pod-network-cidr", podNetworkCIDR, "Specify range of IP addresses for the pod network.")
	cmdCreate.PersistentFlags().StringVar(&serviceCIDR, "service-cidr", serviceCIDR, "Specify range of IP address for service VIPs.")
	cmdCreate.PersistentFlags().IntVar(&controlPlaneMachineCPUCores, "control-plane-machine-cpu-cores", controlPlaneMachineCPUCores, "The CPU cores of each control plane machine.")
	cmdCreate.PersistentFlags().Var(&controlPlaneMachineMemorySize, "control-plane-machine-memory-size", "The memory size of each control plane machine")
	cmdCreate.PersistentFlags().StringVar(&controlPlaneMachineKernelImage, "control-plane-machine-kernel-image", controlPlaneMachineKernelImage, "The kernel image of control plane machine.")
	cmdCreate.PersistentFlags().StringVar(&controlPlaneMachineRootfsImage, "control-plane-machine-rootfs-image", controlPlaneMachineRootfsImage, "The rootfs image of control plane machine.")
	cmdCreate.PersistentFlags().Var(&controlPlaneMachineRootfsSize, "control-plane-machine-rootfs-size", "The rootfs size of each control plane machine.")
	cmdCreate.PersistentFlags().IntVar(&workerMachineCPUCores, "worker-machine-cpu-cores", workerMachineCPUCores, "The CPU cores of each worker machine.")
	cmdCreate.PersistentFlags().Var(&workerMachineMemorySize, "worker-machine-memory-size", "The memory size of each worker machine.")
	cmdCreate.PersistentFlags().StringVar(&workerMachineKernelImage, "worker-machine-kernel-image", workerMachineKernelImage, "The kernel image of worker machine.")
	cmdCreate.PersistentFlags().StringVar(&workerMachineRootfsImage, "worker-machine-rootfs-image", workerMachineRootfsImage, "The rootfs image of worker machine.")
	cmdCreate.PersistentFlags().Var(&workerMachineRootfsSize, "worker-machine-rootfs-size", "The rootfs size of each worker machine.")
	cmdCreate.PersistentFlags().BoolVar(&persistent, "persistent", persistent, "The machines of the nested cluster will be persistent, include persistent storage and IP address.")
	cmdCreate.PersistentFlags().StringSliceVar(&machineAddresses, "machine-addresses", machineAddresses, "The candidate IP addresses for persistent machines of nested cluster.")
	cmdCreate.PersistentFlags().StringVar(&hostClusterCNI, "host-cluster-cni", hostClusterCNI, "The CNI of the host cluster, support 'calico' and 'kube-ovn'.")
	cmdCreate.PersistentFlags().StringVar(&from, "from", from, fmt.Sprintf("The URL of the cluster template to use for the nested cluster. If unspecified, the cluster template of cluster-api-provider-virtink %s will be used.", VirtinkProviderVersion))

	cmdDelete := &cobra.Command{
		Use:   "delete CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a nested cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			if err := runCommand(exec.Command("kubectl", "delete", "clusters.cluster.x-k8s.io", clusterName, "--namespace", targetNamespace, "--wait", "--ignore-not-found")); err != nil {
				return fmt.Errorf("delete cluster CR: %s", err)
			}

			if err := runCommand(exec.Command("kubectl", "delete", "ippool.ipam.metal3.io", clusterName, "--namespace", targetNamespace, "--wait", "--ignore-not-found")); err != nil {
				return fmt.Errorf("delete IPPool CR: %s", err)
			}

			os.Remove(filepath.Join(homedir.HomeDir(), ".kube", fmt.Sprintf("knest.%s.%s.kubeconfig", targetNamespace, args[0])))
			return nil
		},
	}

	cmdList := &cobra.Command{
		Use:   "list",
		Short: "List nested clusters.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runCommand(exec.Command("kubectl", "get", "clusters.cluster.x-k8s.io", "--namespace", targetNamespace, "-o", "wide")); err != nil {
				return fmt.Errorf("list cluster CRs: %s", err)
			}
			return nil
		},
	}

	cmdScale := &cobra.Command{
		Use:   "scale CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Scale a nested cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if newControlPlaneMachineCount > 0 {
				if err := runCommand(exec.Command("kubectl", "patch", "kubeadmcontrolplane.controlplane.cluster.x-k8s.io", fmt.Sprintf("%s-cp", args[0]),
					"--namespace", targetNamespace, "--type", "merge", "--patch", fmt.Sprintf("{\"spec\":{\"replicas\":%d}}", newControlPlaneMachineCount))); err != nil {
					return fmt.Errorf("set control-plane replicas: %s", err)
				}
			}

			if newWorkerMachineCount >= 0 {
				if err := runCommand(exec.Command("kubectl", "patch", "machinedeployment.cluster.x-k8s.io", fmt.Sprintf("%s-md-0", args[0]),
					"--namespace", targetNamespace, "--type", "merge", "--patch", fmt.Sprintf("{\"spec\":{\"replicas\":%d}}", newWorkerMachineCount))); err != nil {
					return fmt.Errorf("set worker replicas: %s", err)
				}
			}
			return nil
		},
	}
	cmdScale.PersistentFlags().IntVar(&newControlPlaneMachineCount, "control-plane-machine-count", newControlPlaneMachineCount, "The number of control plane machines for the nested cluster.")
	cmdScale.PersistentFlags().IntVar(&newWorkerMachineCount, "worker-machine-count", newWorkerMachineCount, "The number of worker machines for the nested cluster.")

	var versionOutput string
	cmdVersion := &cobra.Command{
		Use:   "version",
		Short: "Print knest version.",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := Version{
				Knest:                     version,
				Virtink:                   VirtinkVersion,
				ClusterAPIProviderVirtink: VirtinkProviderVersion,
			}

			switch versionOutput {
			case "":
				fmt.Printf("knest version: %#v, Virtink version: %#v, cluster-api-provider-virtink version: %#v\n", v.Knest, v.Virtink, v.ClusterAPIProviderVirtink)
			case "json":
				data, err := json.MarshalIndent(v, "", "	")
				if err != nil {
					return err
				}
				fmt.Printf("%s\n", data)
			default:
				return fmt.Errorf("unsupported output format: %s", versionOutput)
			}
			return nil
		},
	}
	cmdVersion.PersistentFlags().StringVarP(&versionOutput, "output", "o", versionOutput, "Output format; available options are 'json'")

	rootCmd := &cobra.Command{
		Use:          "knest",
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().StringVarP(&targetNamespace, "target-namespace", "n", targetNamespace, "The namespace to use for the nested cluster.")
	rootCmd.AddCommand(cmdCreate)
	rootCmd.AddCommand(cmdDelete)
	rootCmd.AddCommand(cmdList)
	rootCmd.AddCommand(cmdScale)
	rootCmd.AddCommand(cmdVersion)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func setupClusterctlConfig() error {
	type repository struct {
		Name string `yaml:"Name,omitempty"`
	}

	buf := &bytes.Buffer{}
	repositoriesCmd := exec.Command("clusterctl", "config", "repositories", "-o", "yaml")
	repositoriesCmd.Stdout = buf
	if err := runCommand(repositoriesCmd); err != nil {
		return err
	}

	repositories := []repository{}
	if err := yaml.NewDecoder(buf).Decode(&repositories); err != nil {
		return err
	}

	for _, repository := range repositories {
		if repository.Name == "virtink" {
			return nil
		}
	}

	type provider struct {
		Name string `json:"name,omitempty"`
		URL  string `json:"url,omitempty"`
		Type string `json:"type,omitempty"`
	}

	clusterctlConfig := viper.New()
	clusterctlConfigDir := filepath.Join(homedir.HomeDir(), ".cluster-api")
	clusterctlConfig.AddConfigPath(clusterctlConfigDir)
	clusterctlConfig.SetConfigName("clusterctl")
	clusterctlConfig.SetConfigType("yaml")
	if err := clusterctlConfig.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			if err := os.MkdirAll(clusterctlConfigDir, 0755); err != nil {
				return fmt.Errorf("create clusterctl config dir: %s", err)
			}
		} else {
			return fmt.Errorf("read clusterctl config: %s", err)
		}
	}

	var providers []provider
	if err := clusterctlConfig.UnmarshalKey("providers", &providers); err != nil {
		return fmt.Errorf("unmarshal providers: %s", err)
	}

	for _, provider := range providers {
		if provider.Name == "virtink" {
			return nil
		}
	}

	providers = append(providers, provider{
		Name: "virtink",
		URL:  "https://github.com/smartxworks/cluster-api-provider-virtink/releases/latest/infrastructure-components.yaml",
		Type: "InfrastructureProvider",
	})
	clusterctlConfig.Set("providers", providers)
	if err := clusterctlConfig.WriteConfigAs(filepath.Join(clusterctlConfigDir, "clusterctl.yaml")); err != nil {
		return fmt.Errorf("write clusterctl config: %s", err)
	}
	return nil
}

func runCommand(cmd *exec.Cmd) error {
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run command %q: %s", cmd.String(), err)
	}
	return nil
}

func getCommandOutput(cmd *exec.Cmd) (string, error) {
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("run command %q: %s: %s", cmd, err, output)
	}
	return output, nil
}
