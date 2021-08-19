package rotator

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/drain"
)

var (
	DefaultNodeAwaitJoinTimeout      = 30 * time.Second
	DefaultNodeAwaitReadinessTimeout = 10 * time.Second
)

func GetClusterConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

func NewKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := GetClusterConfig()
	if err != nil {
		return nil, err
	}
	k8s, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return k8s, nil
}

func GetClusterNodeSet(ctx context.Context, k8s *kubernetes.Clientset) (sets.String, error) {
	nodes, err := getClusterNodes(ctx, k8s)
	if err != nil {
		return nil, err
	}
	set := sets.NewString()
	for _, node := range nodes {
		set.Insert(string(node.UID))
	}
	return set, nil
}

func AwaitNewNodeReady(ctx context.Context, k8s *kubernetes.Clientset, nodes sets.String) error {
	errors := make(chan error)
	go func() { errors <- awaitNewNodeReady(ctx, k8s, nodes) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errors:
		return err
	}
}

func awaitNewNodeReady(ctx context.Context, k8s *kubernetes.Clientset, nodes sets.String) error {
	node, err := awaitNewNodeJoin(ctx, k8s, nodes)
	if err != nil {
		return err
	}
	if err := awaitNodeReadiness(ctx, k8s, node); err != nil {
		return err
	}
	return nil
}

func awaitNewNodeJoin(ctx context.Context, k8s *kubernetes.Clientset, known sets.String) (*coreV1.Node, error) {
	for {
		log.Printf("Waiting %s for new node to join cluster...", DefaultNodeAwaitJoinTimeout.String())
		time.Sleep(DefaultNodeAwaitJoinTimeout)

		nodes, err := getClusterNodes(ctx, k8s)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if known.Has(string(node.UID)) {
				continue
			}
			log.Printf("Node '%s' joined cluster.", node.Name)
			return node, nil
		}
	}
}

func awaitNodeReadiness(ctx context.Context, k8s *kubernetes.Clientset, node *coreV1.Node) error {
	for {
		log.Printf("Waiting %s for new node to be ready...", DefaultNodeAwaitReadinessTimeout.String())
		time.Sleep(DefaultNodeAwaitReadinessTimeout)

		n, err := k8s.CoreV1().Nodes().Get(ctx, node.Name, v1.GetOptions{})
		if err != nil {
			return err
		}
		for _, c := range n.Status.Conditions {
			if c.Type == coreV1.NodeReady && c.Status == coreV1.ConditionTrue {
				return nil
			}
		}
	}
}

func GetNodeByInstanceID(ctx context.Context, k8s *kubernetes.Clientset, id string) (*coreV1.Node, error) {
	nodes, err := getClusterNodes(ctx, k8s)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if strings.HasSuffix(node.Spec.ProviderID, id) {
			return node, nil
		}
	}
	return nil, fmt.Errorf("node '%s' is not part of the cluster", id)
}

func getClusterNodes(ctx context.Context, k8s *kubernetes.Clientset) ([]*coreV1.Node, error) {
	list, err := k8s.CoreV1().Nodes().List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	nodes := make([]*coreV1.Node, 0, len(list.Items))
	for _, node := range list.Items {
		n := node
		nodes = append(nodes, &n)
	}
	return nodes, nil
}

func getDrainHelper(ctx context.Context, k8s *kubernetes.Clientset) *drain.Helper {
	return &drain.Helper{
		Ctx:                 ctx,
		Client:              k8s,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		Out:                 os.Stdout,
		ErrOut:              os.Stdout,
		DeleteEmptyDirData:  true,
		Timeout:             time.Duration(600) * time.Second,
	}
}

func DrainNode(ctx context.Context, k8s *kubernetes.Clientset, node *coreV1.Node) error {
	log.Printf("Draining node '%s'.", node.Name)
	helper := getDrainHelper(ctx, k8s)
	err := drain.RunNodeDrain(helper, node.Name)
	if err != nil {
		return err
	}
	return nil
}

func CordonNode(ctx context.Context, k8s *kubernetes.Clientset, node *coreV1.Node) error {
	log.Printf("Cordoning node '%s'.", node.Name)
	helper := getDrainHelper(ctx, k8s)
	err := drain.RunCordonOrUncordon(helper, node, true)
	if err != nil {
		return err
	}
	return nil
}
