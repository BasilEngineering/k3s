package wireguard

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/flannel-io/flannel/pkg/lease"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var KubeConfig string

type kubeClient struct {
	nodeName string
	client   *clientset.Clientset
}

func newKubeClient(ctx context.Context, kubeconfig string) (*kubeClient, error) {
	var cfg *rest.Config
	var err error
	// Try to build kubernetes config from a master url or a kubeconfig filepath. If neither masterUrl
	// or kubeconfigPath are passed in we fall back to inClusterConfig. If inClusterConfig fails,
	// we fallback to the default config.
	cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("fail to create kubernetes config: %v", err)
	}

	c, err := clientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize client: %v", err)
	}

	// The kube subnet mgr needs to know the k8s node name that it's running on so it can annotate it.
	// If we're running as a pod then the POD_NAME and POD_NAMESPACE will be populated and can be used to find the node
	// name. Otherwise, the environment variable NODE_NAME can be passed in.
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		podName := os.Getenv("POD_NAME")
		podNamespace := os.Getenv("POD_NAMESPACE")
		if podName == "" || podNamespace == "" {
			return nil, fmt.Errorf("env variables POD_NAME and POD_NAMESPACE must be set")
		}

		pod, err := c.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("error retrieving pod spec for '%s/%s': %v", podNamespace, podName, err)
		}
		nodeName = pod.Spec.NodeName
		if nodeName == "" {
			return nil, fmt.Errorf("node name not present in pod spec '%s/%s'", podNamespace, podName)
		}
	}
	return &kubeClient{client: c, nodeName: nodeName}, nil
}

func (k *kubeClient) Nodes(ctx context.Context) ([]corev1.Node, error) {
	nl, err := k.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nl.Items, nil
}

func selectNodeByEvent(nodes []corev1.Node, event *lease.Event) *corev1.Node {
	ip4 := event.Lease.Subnet.IP.String()
	publicIp := event.Lease.Attrs.PublicIP.String()
	for _, n := range nodes {
		for _, p := range n.Status.Addresses {
			if p.Address == ip4 || p.Address == publicIp {
				return &n
			}
		}
	}
	for _, n := range nodes {
		for _, p := range n.Spec.PodCIDRs {
			if strings.HasPrefix(p, ip4) {
				return &n
			}
		}
	}
	return nil
}

func selectNodeByName(nodes []corev1.Node, name string) *corev1.Node {
	for _, n := range nodes {
		if n.Name == name {
			return &n
		}
	}
	return nil
}

func isNodeEdge(node *corev1.Node) bool {
	a1 := node.Labels["node.kubernetes.io/edge"]
	a2 := node.Labels["basil.com.tr/edge"]
	return a1 == "true" || a2 == "true"
}

func isNodeCloud(node *corev1.Node) bool {
	a1 := node.Labels["node.kubernetes.io/edge"]
	a2 := node.Labels["basil.com.tr/edge"]
	return a1 == "false" || a2 == "false"
}
func addressToIpNet(a corev1.NodeAddress) *net.IPNet {
	ip := net.ParseIP(a.Address)
	if len(ip) == net.IPv4len {
		return &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(32, 32),
		}
	}
	return &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(128, 128),
	}
}
func addressToIpNet4(a corev1.NodeAddress) *net.IPNet {
	ip := net.ParseIP(a.Address)
	if len(ip) != net.IPv4len {
		return &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(32, 32),
		}
	}
	return nil
}

func addressToIpNet6(a corev1.NodeAddress) *net.IPNet {
	ip := net.ParseIP(a.Address)
	if len(ip) == net.IPv6len {
		return &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(128, 128),
		}
	}
	return nil
}

func externalAddressOf(node *corev1.Node) string {
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeExternalIP {
			return a.Address
		}
	}
	return ""
}

func internalAddressOf(node *corev1.Node) string {
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}
