package rotator

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/exec"
)

type Rotator struct {
	dryrun  bool
	session *session.Session
	asg     *autoscaling.AutoScaling
	ec2     *ec2.EC2
	k8s     *kubernetes.Clientset
}

func NewRotator(dryrun bool) (*Rotator, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	asgClient := autoscaling.New(sess)
	ec2Client := ec2.New(sess)
	k8s, err := NewKubernetesClient()
	if err != nil {
		return nil, err
	}

	r := &Rotator{
		dryrun:  dryrun,
		session: sess,
		asg:     asgClient,
		ec2:     ec2Client,
		k8s:     k8s,
	}
	return r, nil
}

func (r *Rotator) RotateAll(ctx context.Context, groups []string) error {
	for _, group := range groups {
		if err := r.Rotate(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

func (r *Rotator) RotateForCluster(ctx context.Context) error {
	eksClient := eks.New(r.session)

	k8sConfig, err := GetClusterConfig()
	if err != nil {
		return err
	}

	eksCluster, err := getEKSCluserByURL(eksClient, k8sConfig.Host)
	if err != nil {
		return err
	}

	ownerKey := fmt.Sprintf("k8s.io/cluster/%s", *eksCluster.Name)

	groups, err := getAllAutoScalingGroups(r.asg)
	if err != nil {
		return err
	}
	found := false
	for _, group := range groups {
		for _, tag := range group.Tags {
			if *tag.Key == ownerKey && *tag.Value == "owned" {
				log.Printf("ASG %s is owned by cluster %s.\n", *group.AutoScalingGroupName, *eksCluster.Name)
				found = true
				if err := r.Rotate(ctx, *group.AutoScalingGroupName); err != nil {
					return err
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("no ASGs found for cluster %s", *eksCluster.Name)
	}
	return nil
}

func (r *Rotator) Rotate(ctx context.Context, groupId string) error {
	group, err := DescribeAutoScalingGroup(r.asg, groupId)
	if err != nil {
		return err
	}

	log.Printf("Rotating ASG '%s'...\n", groupId)
	for _, id := range group.instanceIds {
		if err := r.RotateInstance(ctx, groupId, id, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *Rotator) RotateByInternalDNS(ctx context.Context, instanceInternalIP string, removeNode bool) error {
	instanceID, groupID, err := DescribeInstanceByInternalDNS(r.ec2, r.asg, instanceInternalIP)
	if err != nil {
		return err
	}
	return r.RotateInstance(ctx, groupID, instanceID, removeNode)
}

func (r *Rotator) RotateInstance(
	ctx context.Context,
	groupId string,
	instanceId string,
	removeNode bool,
) error {
	node, err := GetNodeByInstanceID(ctx, r.k8s, instanceId)
	if err != nil {
		return err
	}

	log.Printf("Rotating node %s (instance %s).\n", node.Name, instanceId)

	if r.dryrun {
		log.Println("DRY RUN is enabled. Skipping rotate.")
		return nil
	}

	if err := CordonNode(ctx, r.k8s, node); err != nil {
		return err
	}
	nodeSet, err := GetClusterNodeSet(ctx, r.k8s)
	if err != nil {
		return err
	}
	if err := DetachInstance(r.asg, groupId, instanceId, removeNode); err != nil {
		return err
	}

	if !removeNode {
		if err := AwaitNewNodeReady(ctx, r.k8s, nodeSet); err != nil {
			return err
		}
	}

	if err := DrainNode(ctx, r.k8s, node); err != nil {
		return err
	}
	if err := TerminateInstanceByID(r.ec2, instanceId); err != nil {
		return err
	}
	return nil
}
