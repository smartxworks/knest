package main

import (
	"embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"text/template"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	rootCmd := &cobra.Command{
		Use: "knest",
	}

	clusterCreateCmd := &cobra.Command{
		Use:  "create CLUSTER",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			kubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
			if err != nil {
				return fmt.Errorf("load kubeconfig: %s", err)
			}

			kubeClient, err := kubernetes.NewForConfig(kubeconfig)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %s", err)
			}

			components := []struct {
				crdName                string
				requiredDeploymentKeys []types.NamespacedName
				manifestName           string
			}{{
				crdName:      "certificates.cert-manager.io",
				manifestName: "cert-manager.yaml",
			}, {
				crdName: "virtualmachines.virt.virtink.smartx.com",
				requiredDeploymentKeys: []types.NamespacedName{{
					Name:      "cert-manager-webhook",
					Namespace: "cert-manager",
				}},
				manifestName: "virtink.yaml",
			}, {
				crdName:      "clusters.cluster.x-k8s.io",
				manifestName: "cluster-api-components.yaml",
			}, {
				crdName:      "virtinkclusters.infrastructure.cluster.x-k8s.io",
				manifestName: "capch.yaml",
			}}

			for _, component := range components {
				output, err := exec.Command("kubectl", "get", "crds", component.crdName, "--ignore-not-found").CombinedOutput()
				if err != nil {
					return fmt.Errorf("get CRD: %s", err)
				}

				if len(output) == 0 {
					for _, k := range component.requiredDeploymentKeys {
						if err := exec.Command("kubectl", "wait", "-n", k.Namespace, "deployments", k.Name, "--for=condition=Available", "--timeout=-1s").Run(); err != nil {
							return fmt.Errorf("wait for deployment: %s", err)
						}
					}

					if err := applyManifest(component.manifestName); err != nil {
						return fmt.Errorf("apply manifest: %s", err)
					}
				}
			}

			capiWebhookDeploymentKeys := []types.NamespacedName{{
				Name:      "capi-controller-manager",
				Namespace: "capi-system",
			}, {
				Name:      "capi-kubeadm-bootstrap-controller-manager",
				Namespace: "capi-kubeadm-bootstrap-system",
			}, {
				Name:      "capi-kubeadm-control-plane-controller-manager",
				Namespace: "capi-kubeadm-control-plane-system",
			}, {
				Name:      "capch-controller-manager",
				Namespace: "capch-system",
			}}
			for _, k := range capiWebhookDeploymentKeys {
				if err := exec.Command("kubectl", "wait", "-n", k.Namespace, "deployments", k.Name, "--for=condition=Available", "--timeout=-1s").Run(); err != nil {
					return fmt.Errorf("wait for deployment: %s", err)
				}
			}

			if err := applyTemplate("cluster.yaml", map[string]interface{}{"name": clusterName}); err != nil {
				return fmt.Errorf("apply cluster template: %s", err)
			}

			if err := exec.Command("kubectl", "wait", "clusters.cluster.x-k8s.io", clusterName, "--for=condition=ControlPlaneInitialized", "--timeout=-1s").Run(); err != nil {
				return fmt.Errorf("wait for cluster: %s", err)
			}

			// TODO: change to kubectl
			clusterService, err := kubeClient.CoreV1().Services("default").Get(cmd.Context(), clusterName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get cluster service: %s", err)
			}

			clusterKubeconfigSecret, err := kubeClient.CoreV1().Secrets("default").Get(cmd.Context(), fmt.Sprintf("%s-kubeconfig", clusterName), metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get cluster kubeconfig: %s", err)
			}

			clusterKubeconfigFileName := fmt.Sprintf("%s.kubeconfig", clusterName)
			if err := os.WriteFile(clusterKubeconfigFileName, clusterKubeconfigSecret.Data["value"], 0644); err != nil {
				return fmt.Errorf("save cluster kubeconfig: %s", err)
			}

			u, err := url.Parse(kubeconfig.Host)
			if err != nil {
				panic(err)
			}

			modifyKubeconfigCmd := exec.Command("kubectl", "config", "--kubeconfig", clusterKubeconfigFileName, "set-cluster", clusterName,
				"--server", fmt.Sprintf("%s://%s:%d", u.Scheme, u.Hostname(), clusterService.Spec.Ports[0].NodePort),
				"--tls-server-name", clusterService.Spec.ClusterIP)
			output, err := modifyKubeconfigCmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("modify cluster kubeconfig: %q: %s: %s", modifyKubeconfigCmd, err, string(output))
			}
			return nil
		},
	}

	rootCmd.AddCommand(clusterCreateCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

//go:embed manifests
var manifests embed.FS

func applyManifest(name string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	w, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("get kubectl input pipe: %s", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run kubectl: %s", err)
	}
	defer cmd.Wait()
	defer w.Close()

	c, err := manifests.ReadFile("manifests/" + name)
	if err != nil {
		return fmt.Errorf("read manifest: %s", err)
	}

	if _, err := w.Write(c); err != nil {
		return fmt.Errorf("write manifest: %s", err)
	}
	return nil
}

//go:embed templates
var templates embed.FS

func applyTemplate(name string, data interface{}) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	w, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("get kubectl input pipe: %s", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run kubectl: %s", err)
	}
	defer cmd.Wait()
	defer w.Close()

	t, err := template.ParseFS(templates, "templates/"+name)
	if err != nil {
		return fmt.Errorf("parse template: %s", err)
	}

	if err := t.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute template: %s", err)
	}
	return nil
}
