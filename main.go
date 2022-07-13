package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
)

type createClusterOptions struct {
	clusterName               string
	from                      string
	kubernetesVersion         string
	controlPlaneMachineCount  int
	workerMachineCount        int
	virtinkPodNetworkCIDR     string
	virtinkServiceCIDR        string
	virtinkMachineCPUCount    int
	virtinkMachineMemorySize  resource.QuantityValue
	virtinkMachineKernelImage string
	virtinkMachineRootfsImage string
	virtinkMachineRootfsSize  resource.QuantityValue
}

var createClusterOpts = &createClusterOptions{
	kubernetesVersion:         "v1.24.0",
	controlPlaneMachineCount:  3,
	workerMachineCount:        3,
	virtinkPodNetworkCIDR:     "10.17.0.0/16",
	virtinkServiceCIDR:        "10.112.0.0/12",
	virtinkMachineCPUCount:    2,
	virtinkMachineMemorySize:  resource.QuantityValue{Quantity: resource.MustParse("2Gi")},
	virtinkMachineKernelImage: "smartxworks/capch-kernel-5.15.12",
	virtinkMachineRootfsImage: "smartxworks/capch-rootfs-1.24.0",
	virtinkMachineRootfsSize:  resource.QuantityValue{Quantity: resource.MustParse("4Gi")},
}

type scaleClusterOptions struct {
	nodeRole     string
	nodeReplicas int
}

var scaleClusterOpts = &scaleClusterOptions{
	nodeRole:     "all",
	nodeReplicas: 3,
}

func main() {
	rootCmd := &cobra.Command{
		Use: "knest",
	}

	createClusterCmd := &cobra.Command{
		Use:   "create CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Create a workload cluster.",
		RunE:  createCluster,
	}
	createClusterCmd.Flags().StringVar(&createClusterOpts.from, "from", createClusterOpts.from, "The URL to read the workload cluster template from")
	createClusterCmd.Flags().StringVar(&createClusterOpts.kubernetesVersion, "kubernetes-version", createClusterOpts.kubernetesVersion, "The kubernetes version of workload cluster")
	createClusterCmd.Flags().IntVar(&createClusterOpts.controlPlaneMachineCount, "control-plane-machine-count", createClusterOpts.controlPlaneMachineCount, "The control plane machine count of workload cluster")
	createClusterCmd.Flags().IntVar(&createClusterOpts.workerMachineCount, "worker-machine-count", createClusterOpts.workerMachineCount, "The worker machine count of workload cluster")
	createClusterCmd.Flags().StringVar(&createClusterOpts.virtinkPodNetworkCIDR, "virtink-pod-network-cidr", createClusterOpts.virtinkPodNetworkCIDR, "The pod network CIDR of workload cluster")
	createClusterCmd.Flags().StringVar(&createClusterOpts.virtinkServiceCIDR, "virtink-service-cidr", createClusterOpts.virtinkServiceCIDR, "The service CIDR of workload cluster")
	createClusterCmd.Flags().IntVar(&createClusterOpts.virtinkMachineCPUCount, "virtink-machine-cpu-count", createClusterOpts.virtinkMachineCPUCount, "The virtink machine CPU count")
	createClusterCmd.Flags().Var(&createClusterOpts.virtinkMachineMemorySize, "virtink-machine-memory-size", "virtink machine memory size")
	createClusterCmd.Flags().StringVar(&createClusterOpts.virtinkMachineKernelImage, "virtink-machine-kernel-image", createClusterOpts.virtinkMachineKernelImage, "The virtink machine kernel image")
	createClusterCmd.Flags().StringVar(&createClusterOpts.virtinkMachineRootfsImage, "virtink-machine-rootfs-image", createClusterOpts.virtinkMachineRootfsImage, "The virtink machine rootfs image")
	createClusterCmd.Flags().Var(&createClusterOpts.virtinkMachineRootfsSize, "virtink-machine-rootfs-size", "virtink machine rootfs size")
	rootCmd.AddCommand(createClusterCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "delete CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a workload cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			if err := runCommandPiped("kubectl", "delete", "clusters.cluster.x-k8s.io", clusterName, "--wait=true"); err != nil {
				return fmt.Errorf("delete cluster template: %s", err)
			}
			return nil
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workload clusters.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runCommandPiped("kubectl", "get", "clusters.cluster.x-k8s.io", "-o", "wide"); err != nil {
				return fmt.Errorf("list workload clusters: %s", err)
			}
			return nil
		},
	})

	scale := &cobra.Command{
		Use:   "scale CLUSTER",
		Args:  cobra.ExactArgs(1),
		Short: "Scale workload cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			isControlPlane := false
			isWorker := false
			switch scaleClusterOpts.nodeRole {
			case "all":
				isControlPlane = true
				isWorker = true
			case "control-plane":
				isControlPlane = true
			case "worker":
				isWorker = true
			default:
				return fmt.Errorf("unkown role of Node: '%s'", scaleClusterOpts.nodeRole)
			}
			if isControlPlane {
				if err := runCommandPiped("kubectl", "patch", "kubeadmcontrolplane.controlplane.cluster.x-k8s.io", fmt.Sprintf("%s-cp", clusterName), fmt.Sprintf("--patch={\"spec\":{\"replicas\":%v}}", scaleClusterOpts.nodeReplicas), "--type=merge"); err != nil {
					return fmt.Errorf("scale control-plane: %s", err)
				}
			}
			if isWorker {
				if err := runCommandPiped("kubectl", "patch", "machinedeployment.cluster.x-k8s.io", fmt.Sprintf("%s-md", clusterName), fmt.Sprintf("--patch={\"spec\":{\"replicas\":%v}}", scaleClusterOpts.nodeReplicas), "--type=merge"); err != nil {
					return fmt.Errorf("scale worker: %s", err)
				}
			}
			return nil
		},
	}
	scale.Flags().IntVar(&scaleClusterOpts.nodeReplicas, "node-replicas", scaleClusterOpts.nodeReplicas, "Desired number of Node replicas")
	scale.Flags().StringVar(&scaleClusterOpts.nodeRole, "node-role", scaleClusterOpts.nodeRole, "Role of Node (options: \"control-plane\", \"worker\", \"all\")")
	rootCmd.AddCommand(scale)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type clusterctlConfigProvider struct {
	Name string                    `json:"name,omitempty"`
	URL  string                    `json:"url,omitempty"`
	Type clusterctlv1.ProviderType `json:"type,omitempty"`
}

func ensureInfrastructure() error {
	clusterctlViper := viper.New()
	clusterctlConfigDir := filepath.Join(homedir.HomeDir(), ".cluster-api")
	clusterctlViper.AddConfigPath(clusterctlConfigDir)
	clusterctlViper.SetConfigName("clusterctl")
	clusterctlViper.SetConfigType("yaml")
	if err := clusterctlViper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return err
		}
		if err := os.MkdirAll(clusterctlConfigDir, 0644); err != nil {
			return err
		}
	}
	clusterctlConfigProviders := []clusterctlConfigProvider{}
	if err := clusterctlViper.UnmarshalKey("providers", &clusterctlConfigProviders); err != nil {
		return fmt.Errorf("unmarshal providers: %s", err)
	}
	providerFound := false
	for _, provider := range clusterctlConfigProviders {
		if provider.Name == "virtink" {
			providerFound = true
			break
		}
	}
	if !providerFound {
		virtinkProvider := clusterctlConfigProvider{
			Name: "virtink",
			URL:  "https://github.com/smartxworks/cluster-api-provider-virtink/releases/latest/infrastructure-components.yaml",
			Type: clusterctlv1.InfrastructureProviderType,
		}
		clusterctlConfigProviders = append(clusterctlConfigProviders, virtinkProvider)

		clusterctlViper.Set("providers", clusterctlConfigProviders)
		if err := clusterctlViper.WriteConfigAs(filepath.Join(clusterctlConfigDir, "clusterctl.yaml")); err != nil {
			return fmt.Errorf("write clusterctl config: %s", err)
		}
	}

	if _, err := runCommand("clusterctl", "init", "--infrastructure=virtink", "--wait-providers"); err != nil {
		if !strings.Contains(err.Error(), `there is already an instance of the "infrastructure-virtink" provider installed`) {
			return fmt.Errorf("clusterctl init: %s", err)
		}
	}

	components := []struct {
		name     string
		manifest string
	}{{
		name:     "virtink",
		manifest: "https://github.com/smartxworks/virtink/releases/download/v0.7.1/virtink.yaml",
	}}
	for _, component := range components {
		if err := runCommandPiped("kubectl", "apply", "-f", component.manifest); err != nil {
			return fmt.Errorf("apply component %s: %s", component.name, err)
		}
	}

	webhookDeploymentKeys := []types.NamespacedName{{
		Name:      "virt-controller",
		Namespace: "virtink-system",
	}}
	for _, k := range webhookDeploymentKeys {
		if err := runCommandPiped("kubectl", "wait", "-n", k.Namespace, "deployments", k.Name, "--for=condition=Available", "--timeout=-1s"); err != nil {
			return fmt.Errorf("wait for deployment: %s", err)
		}
	}

	return nil
}

func createCluster(cmd *cobra.Command, args []string) error {
	if err := ensureInfrastructure(); err != nil {
		return err
	}

	createClusterOpts.clusterName = args[0]

	kubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %s", err)
	}

	generateClusterCmdEnvs := []string{"CLUSTERCTL_DISABLE_VERSIONCHECK=true"}
	generateClusterCmdEnvs = append(generateClusterCmdEnvs,
		fmt.Sprintf("KUBERNETES_VERSION=%s", createClusterOpts.kubernetesVersion),
		fmt.Sprintf("CONTROL_PLANE_MACHINE_COUNT=%d", createClusterOpts.controlPlaneMachineCount),
		fmt.Sprintf("WORKER_MACHINE_COUNT=%d", createClusterOpts.workerMachineCount),
		fmt.Sprintf("VIRTINK_POD_NETWORK_CIDR=%s", createClusterOpts.virtinkPodNetworkCIDR),
		fmt.Sprintf("VIRTINK_SERVICE_CIDR=%s", createClusterOpts.virtinkServiceCIDR),
		fmt.Sprintf("VIRTINK_MACHINE_CPU_COUNT=%d", createClusterOpts.virtinkMachineCPUCount),
		fmt.Sprintf("VIRTINK_MACHINE_MEMORY_SIZE=%s", createClusterOpts.virtinkMachineMemorySize.String()),
		fmt.Sprintf("VIRTINK_MACHINE_KERNEL_IMAGE=%s", createClusterOpts.virtinkMachineKernelImage),
		fmt.Sprintf("VIRTINK_MACHINE_ROOTFS_IMAGE=%s", createClusterOpts.virtinkMachineRootfsImage),
		fmt.Sprintf("VIRTINK_MACHINE_ROOTFS_SIZE=%s", createClusterOpts.virtinkMachineRootfsSize.String()),
	)

	generateClusterArgs := []string{"generate", "cluster", createClusterOpts.clusterName}
	if createClusterOpts.from != "" {
		generateClusterArgs = append(generateClusterArgs, "--from", createClusterOpts.from)
	} else {
		generateClusterArgs = append(generateClusterArgs, "--flavor", "mhc")
	}

	clusterData, err := runCommandWithEnvs("clusterctl", generateClusterCmdEnvs, generateClusterArgs...)
	if err != nil {
		return fmt.Errorf("generate cluster: %s", err)
	}
	generatedClusterFileName := fmt.Sprintf("generated-cluster-%s.yaml", createClusterOpts.clusterName)
	if err := os.WriteFile(generatedClusterFileName, []byte(clusterData), 0644); err != nil {
		return fmt.Errorf("save generated cluster: %s", err)
	}
	if err := runCommandPiped("kubectl", "apply", "-f", generatedClusterFileName); err != nil {
		return fmt.Errorf("create cluster: %s", err)
	}

	if err := runCommandPiped("kubectl", "wait", "clusters.cluster.x-k8s.io", createClusterOpts.clusterName, "--for=condition=ControlPlaneInitialized", "--timeout=-1s"); err != nil {
		return fmt.Errorf("wait for cluster: %s", err)
	}

	clusterServiceNodePort, err := runCommand("kubectl", "get", "service", createClusterOpts.clusterName, "-o", "jsonpath={.spec.ports[0].nodePort}")
	if err != nil {
		return fmt.Errorf("get cluster service: %s", err)
	}
	clusterServiceClusterIP, err := runCommand("kubectl", "get", "service", createClusterOpts.clusterName, "-o", "jsonpath={.spec.clusterIP}")
	if err != nil {
		return fmt.Errorf("get cluster service: %s", err)
	}

	clusterKubeconfigData, err := runCommand("kubectl", "get", "secret", fmt.Sprintf("%s-kubeconfig", createClusterOpts.clusterName), "-o", "jsonpath={.data.value}")
	if err != nil {
		return fmt.Errorf("get cluster kubeconfig: %s", err)
	}
	decodedClusterKubeconfigData, err := base64.StdEncoding.DecodeString(clusterKubeconfigData)
	if err != nil {
		return err
	}

	clusterKubeconfigFileName := fmt.Sprintf("%s.kubeconfig", createClusterOpts.clusterName)
	if err := os.WriteFile(clusterKubeconfigFileName, decodedClusterKubeconfigData, 0644); err != nil {
		return fmt.Errorf("save cluster kubeconfig: %s", err)
	}

	u, err := url.Parse(kubeconfig.Host)
	if err != nil {
		return err
	}

	if err := runCommandPiped("kubectl", "config", "--kubeconfig", clusterKubeconfigFileName, "set-cluster", createClusterOpts.clusterName,
		"--server", fmt.Sprintf("%s://%s:%s", u.Scheme, u.Hostname(), clusterServiceNodePort),
		"--tls-server-name", clusterServiceClusterIP); err != nil {
		return fmt.Errorf("modify cluster kubeconfig: %s", err)
	}
	return nil
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, fmt.Errorf("execute command %q: %s: %s", cmd, err, output)
	}
	return output, nil
}

func runCommandPiped(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCommandWithEnvs(name string, envs []string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, envs...)
	out, err := cmd.CombinedOutput()

	output := string(out)
	if err != nil {
		return output, fmt.Errorf("execute command %q: %s: %s", cmd, err, output)
	}
	return output, nil
}
