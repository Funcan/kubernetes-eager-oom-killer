package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func intervalDefault() time.Duration {
	if v := os.Getenv("INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("Invalid INTERVAL env var %q: %v", v, err)
		}
		return d
	}
	return 1 * time.Minute
}

func main() {
	var interval time.Duration
	var kubeconfig string
	flag.DurationVar(&interval, "interval", intervalDefault(), "polling interval (e.g. 30s, 2m); also settable via INTERVAL env var")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (uses in-cluster config if empty)")
	flag.Parse()

	config, err := buildConfig(kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build kube config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create kubernetes client: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("Starting eager-oom-killer with interval %s", interval)

	// Run immediately on startup, then on ticker
	runCheck(ctx, clientset, config)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down")
			return
		case <-ticker.C:
			runCheck(ctx, clientset, config)
		}
	}
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	// Try in-cluster first, fall back to default kubeconfig location
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, nil).ClientConfig()
	}
	return cfg, nil
}

func runCheck(ctx context.Context, clientset kubernetes.Interface, config *rest.Config) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("Error listing nodes: %v", err)
		return
	}

	for _, node := range nodes.Items {
		if err := checkNode(ctx, clientset, config, node.Name); err != nil {
			log.Printf("Error checking node %s: %v", node.Name, err)
		}
	}
}

// checkNode queries the kubelet metrics endpoint on a node and kills any pods
// with containers over their memory limit.
func checkNode(ctx context.Context, clientset kubernetes.Interface, config *rest.Config, nodeName string) error {
	metrics, err := fetchKubeletMetrics(ctx, clientset, nodeName)
	if err != nil {
		return fmt.Errorf("fetching kubelet metrics: %w", err)
	}

	containerMemory := parseContainerMemory(metrics)
	if len(containerMemory) == 0 {
		return nil
	}

	// Get all pods on this node
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return fmt.Errorf("listing pods on node %s: %w", nodeName, err)
	}

	for _, pod := range pods.Items {
		checkPodContainers(ctx, clientset, &pod, containerMemory)
	}

	return nil
}

// fetchKubeletMetrics retrieves the /metrics/resource endpoint from a node's
// kubelet via the Kubernetes API server proxy.
func fetchKubeletMetrics(ctx context.Context, clientset kubernetes.Interface, nodeName string) (string, error) {
	data, err := clientset.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy", "metrics", "resource").
		DoRaw(ctx)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// containerKey uniquely identifies a container on a node.
type containerKey struct {
	namespace string
	pod       string
	container string
}

// parseContainerMemory extracts container_memory_working_set_bytes from
// Prometheus-format kubelet metrics.
func parseContainerMemory(metrics string) map[containerKey]int64 {
	result := make(map[containerKey]int64)
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, "container_memory_working_set_bytes{") {
			continue
		}

		ns := extractLabel(line, "namespace")
		pod := extractLabel(line, "pod")
		container := extractLabel(line, "container")
		if ns == "" || pod == "" || container == "" {
			continue
		}

		// Parse the value after the closing }
		idx := strings.LastIndex(line, "}")
		if idx == -1 || idx+1 >= len(line) {
			continue
		}
		valueStr := strings.TrimSpace(line[idx+1:])
		// Strip any timestamp suffix
		if spaceIdx := strings.Index(valueStr, " "); spaceIdx != -1 {
			valueStr = valueStr[:spaceIdx]
		}

		var value float64
		if _, err := fmt.Sscanf(valueStr, "%g", &value); err != nil {
			continue
		}

		result[containerKey{namespace: ns, pod: pod, container: container}] = int64(value)
	}
	return result
}

func extractLabel(line, label string) string {
	key := label + `="`
	idx := strings.Index(line, key)
	if idx == -1 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(line[start:], `"`)
	if end == -1 {
		return ""
	}
	return line[start : start+end]
}

func checkPodContainers(ctx context.Context, clientset kubernetes.Interface, pod *corev1.Pod, containerMemory map[containerKey]int64) {
	for _, container := range pod.Spec.Containers {
		limit := container.Resources.Limits[corev1.ResourceMemory]
		if limit.IsZero() {
			continue
		}
		limitBytes := limit.Value()

		key := containerKey{
			namespace: pod.Namespace,
			pod:       pod.Name,
			container: container.Name,
		}
		usageBytes, ok := containerMemory[key]
		if !ok {
			continue
		}

		if usageBytes > limitBytes {
			log.Printf("Container %s/%s/%s using %s, limit %s — killing pod",
				pod.Namespace, pod.Name, container.Name,
				resource.NewQuantity(usageBytes, resource.BinarySI).String(),
				limit.String())

			if err := killPod(ctx, clientset, pod, container.Name, usageBytes, limitBytes); err != nil {
				log.Printf("Error killing pod %s/%s: %v", pod.Namespace, pod.Name, err)
			}
			return // Pod is deleted, no need to check remaining containers
		}
	}
}

func killPod(ctx context.Context, clientset kubernetes.Interface, pod *corev1.Pod, containerName string, usageBytes, limitBytes int64) error {
	// Create an event before deleting
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "eager-oom-kill-",
			Namespace:    pod.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Pod",
			Namespace:  pod.Namespace,
			Name:       pod.Name,
			UID:        pod.UID,
			APIVersion: "v1",
		},
		Reason:  "EagerOOMKilling",
		Message: fmt.Sprintf("Container %s using %s which exceeds limit %s", containerName, resource.NewQuantity(usageBytes, resource.BinarySI).String(), resource.NewQuantity(limitBytes, resource.BinarySI).String()),
		Type:    "Warning",
		Source: corev1.EventSource{
			Component: "eager-oom-killer",
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Count:          1,
	}

	if _, err := clientset.CoreV1().Events(pod.Namespace).Create(ctx, event, metav1.CreateOptions{}); err != nil {
		log.Printf("Warning: failed to create event for %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	return clientset.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
}
