package rotator

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/exec"
)

type Rotator struct {
	dryrun  bool
	limit   uint
	session *session.Session
	asg     *autoscaling.AutoScaling
	ec2     *ec2.EC2
	k8s     *kubernetes.Clientset
}

func NewRotator(dryrun bool, limit uint) (*Rotator, error) {
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
		limit:   limit,
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

	eksCluster, err := GetEKSCluserByURL(eksClient, k8sConfig.Host)
	if err != nil {
		return err
	}

	ownerKey := fmt.Sprintf("k8s.io/cluster/%s", *eksCluster.Name)

	groups, err := GetAllAutoScalingGroups(r.asg)
	if err != nil {
		return err
	}
	found := false
	var instanceGroups InstanceGroups
	for _, group := range groups {
		for _, tag := range group.Tags {
			if *tag.Key == ownerKey && *tag.Value == "owned" {
				log.Printf("ASG '%s' is owned by cluster '%s'.\n", *group.AutoScalingGroupName, *eksCluster.Name)
				found = true
				igs, err := GetInstancesForGroup(r.ec2, group)
				if err != nil {
					return err
				}
				instanceGroups = append(instanceGroups, igs...)
				break
			}
		}
	}
	if !found {
		return fmt.Errorf("no ASGs found for cluster '%s'", *eksCluster.Name)
	}
	return r.RotateInstanceGroups(ctx, instanceGroups)
}

func (r *Rotator) Rotate(ctx context.Context, groupId string) error {
	instanceGroups, err := DescribeAutoScalingGroup(r.asg, r.ec2, groupId)
	if err != nil {
		return err
	}
	log.Printf("Rotating ASG '%s'...\n", groupId)
	return r.RotateInstanceGroups(ctx, instanceGroups)
}

func (r *Rotator) RotateInstanceGroups(ctx context.Context, instanceGroups InstanceGroups) error {
	sort.Sort(ByAge{instanceGroups})
	if r.limit > 0 {
		instanceGroups = instanceGroups[:r.limit]
	}

	log.Printf("Rotating %d nodes, oldest to newest.", len(instanceGroups))
	for _, group := range instanceGroups {
		if err := r.RotateInstance(ctx, group, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *Rotator) RotateByInternalDNS(ctx context.Context, instanceInternalIP string, removeNode bool) error {
	instanceGroup, err := DescribeInstanceByInternalDNS(r.ec2, r.asg, instanceInternalIP)
	if err != nil {
		return err
	}
	return r.RotateInstance(ctx, instanceGroup, removeNode)
}

func (r *Rotator) RotateInstance(
	ctx context.Context,
	instanceGroup *InstanceGroup,
	removeNode bool,
) error {
	instanceId := instanceGroup.instanceId()
	groupId := instanceGroup.groupId()

	node, err := GetNodeByInstanceID(ctx, r.k8s, instanceId)
	if err != nil {
		return err
	}

	log.Printf("Rotating node '%s' (instance '%s').\n", node.Name, instanceId)

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
