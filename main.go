package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	VirtinkVersion         = "v0.10.1"
	VirtinkProviderVersion = "v0.4.0"
)

func main() {
	var (
		targetNamespace                          = "default"
		kubernetesVersion                        = "1.24.0"
		controlPlaneMachineCount                 = 1
		workerMachineCount                       = 1
		podNetworkCIDR                           = "192.168.0.0/16"
		serviceCIDR                              = "10.96.0.0/12"
		controlPlaneMachineCPUCores              = 2
		controlPlaneMachineMemorySize            = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		controlPlaneMachineKernelImage           = "smartxworks/capch-kernel-5.15.12"
		controlPlaneMachineRootfsImage           = "smartxworks/capch-rootfs-1.24.0"
		controlPlaneMachineRootfsSize            = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		workerMachineCPUCores                    = 2
		workerMachineMemorySize                  = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		workerMachineKernelImage                 = "smartxworks/capch-kernel-5.15.12"
		workerMachineRootfsImage                 = "smartxworks/capch-rootfs-1.24.0"
		workerMachineRootfsSize                  = resource.QuantityValue{Quantity: resource.MustParse("4Gi")}
		persistent                               = false
		persistentControlPlaneMachineRootfsImage = "smartxworks/capch-rootfs-cdi-1.24.0"
		persistentWorkerMachineRootfsImage       = "smartxworks/capch-rootfs-cdi-1.24.0"
		persistentMachineAddresses               []string
		persistentMachineAnnotations             []string
		clusterTemplate                          string
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
				return fmt.Errorf("get Cluster API CRDs: %s", err)
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
			generateCmd.Env = append(os.Environ(),
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

			if clusterTemplate != "" {
				generateCmd.Args = append(generateCmd.Args, "--from", clusterTemplate)
			} else {
				generateCmd.Args = append(generateCmd.Args, "--infrastructure", fmt.Sprintf("virtink:%s", VirtinkProviderVersion))
				if persistent {
					generateCmd.Args = append(generateCmd.Args, "--flavor", "cdi-internal")
				} else {
					generateCmd.Args = append(generateCmd.Args, "--flavor", "internal")
				}
			}

			if persistent {
				generateCmd.Env = append(os.Environ(),
					fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_ROOTFS_CDI_IMAGE=%s", persistentControlPlaneMachineRootfsImage),
					fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_CDI_IMAGE=%s", persistentWorkerMachineRootfsImage),
					fmt.Sprintf("VIRTINK_NODE_ADDRESSES=[%v]", strings.Join(persistentMachineAddresses, ",")),
					fmt.Sprintf("VIRTINK_NODE_ADDRESS_ANNOTATIONS=%s", func() string {
						quotedAnnotations := []string{}
						for i := range persistentMachineAnnotations {
							quotedAnnotations = append(quotedAnnotations, fmt.Sprintf("%q", persistentMachineAnnotations[i]))
						}
						return fmt.Sprintf("[%v]", strings.Join(quotedAnnotations, ","))
					}()))
			} else {
				generateCmd.Env = append(os.Environ(),
					fmt.Sprintf("VIRTINK_CONTROL_PLANE_MACHINE_ROOTFS_IMAGE=%s", controlPlaneMachineRootfsImage),
					fmt.Sprintf("VIRTINK_WORKER_MACHINE_ROOTFS_IMAGE=%s", workerMachineRootfsImage))
			}
			if err := pipeCommands(generateCmd, exec.Command("kubectl", "apply", "-f", "-")); err != nil {
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
	cmdCreate.PersistentFlags().StringVar(&persistentControlPlaneMachineRootfsImage, "persistent-control-plane-machine-rootfs-image", persistentControlPlaneMachineRootfsImage, "The rootfs image of persisten control plane machine.")
	cmdCreate.PersistentFlags().StringVar(&persistentWorkerMachineRootfsImage, "persistent-worker-machine-rootfs-image", persistentWorkerMachineRootfsImage, "The rootfs image of persistent worker machine.")
	cmdCreate.PersistentFlags().StringSliceVar(&persistentMachineAddresses, "persistent-machine-addresses", persistentMachineAddresses, "The candidate IP addresses for persistent machines of nested cluster.")
	cmdCreate.PersistentFlags().StringArrayVar(&persistentMachineAnnotations, "persistent-machine-annotations", persistentMachineAnnotations,
		"The host cluster CNI required annotations to specify IP and MAC address for pod, can use '$IP_ADDRESS' and '$MAC_ADDRESS' as placeholders which will be replaced by allocated IP and MAC address.")
	cmdCreate.PersistentFlags().StringVar(&clusterTemplate, "cluster-template", clusterTemplate, fmt.Sprintf("The URL of the cluster template to use for the nested cluster. If unspecified, the cluster template of cluster-api-provider-virtink %s will be used.", VirtinkProviderVersion))

	cmdDelete := &cobra.Command{
		Use:   "delete CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a nested cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			if err := runCommand(exec.Command("kubectl", "delete", "clusters.cluster.x-k8s.io", clusterName, "--namespace", targetNamespace, "--wait")); err != nil {
				return fmt.Errorf("delete cluster CR: %s", err)
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
		Use:   "scale CLUSTER CONTROL_PLANE_MACHINE_COUNT:WORKER_MACHINE_COUNT",
		Args:  cobra.ExactArgs(2),
		Short: "Scale a nested cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			counts := strings.Split(args[1], ":")
			if len(counts) != 2 {
				return fmt.Errorf("invalid machine counts argument")
			}

			if counts[0] != "" {
				if err := runCommand(exec.Command("kubectl", "patch", "kubeadmcontrolplane.controlplane.cluster.x-k8s.io", fmt.Sprintf("%s-cp", args[0]),
					"--namespace", targetNamespace, "--type", "merge", "--patch", fmt.Sprintf("{\"spec\":{\"replicas\":%s}}", counts[0]))); err != nil {
					return fmt.Errorf("set control-plane replicas: %s", err)
				}
			}

			if counts[1] != "" {
				if err := runCommand(exec.Command("kubectl", "patch", "machinedeployment.cluster.x-k8s.io", fmt.Sprintf("%s-md-0", args[0]),
					"--namespace", targetNamespace, "--type", "merge", "--patch", fmt.Sprintf("{\"spec\":{\"replicas\":%s}}", counts[1]))); err != nil {
					return fmt.Errorf("set worker replicas: %s", err)
				}
			}
			return nil
		},
	}

	rootCmd := &cobra.Command{
		Use: "knest",
	}
	rootCmd.PersistentFlags().StringVarP(&targetNamespace, "target-namespace", "n", targetNamespace, "The namespace to use for the nested cluster.")
	rootCmd.AddCommand(cmdCreate)
	rootCmd.AddCommand(cmdDelete)
	rootCmd.AddCommand(cmdList)
	rootCmd.AddCommand(cmdScale)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func setupClusterctlConfig() error {
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
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

func pipeCommands(cmd1 *exec.Cmd, cmd2 *exec.Cmd) error {
	cmd1.Stdin = os.Stdin
	cmd1.Stderr = os.Stderr
	cmd2.Stdin, _ = cmd1.StdoutPipe()
	cmd2.Stdout = os.Stdout
	cmd2.Stderr = os.Stderr

	if err := cmd1.Start(); err != nil {
		return fmt.Errorf("start command %q: %s", cmd1.String(), err)
	}

	if err := cmd2.Run(); err != nil {
		return fmt.Errorf("run command %q: %s", cmd2.String(), err)
	}

	if err := cmd1.Wait(); err != nil {
		return fmt.Errorf("wait command %q: %s", cmd1.String(), err)
	}
	return nil
}
