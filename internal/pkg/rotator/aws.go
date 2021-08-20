package rotator

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

type InstanceGroup struct {
	instance *ec2.Instance
	group    *autoscaling.Group
}

func (ig InstanceGroup) instanceId() string { return *ig.instance.InstanceId }
func (ig InstanceGroup) groupId() string    { return *ig.group.AutoScalingGroupName }

type InstanceGroups []*InstanceGroup

func (ig InstanceGroups) Len() int      { return len(ig) }
func (ig InstanceGroups) Swap(i, j int) { ig[i], ig[j] = ig[j], ig[i] }

type ByAge struct{ InstanceGroups }

func (ig ByAge) Less(i, j int) bool {
	return ig.InstanceGroups[i].instance.LaunchTime.Before(*ig.InstanceGroups[j].instance.LaunchTime)
}

func GetInstancesForGroup(ec2Client *ec2.EC2, group *autoscaling.Group) (InstanceGroups, error) {
	instanceGroups := make(InstanceGroups, 0, len(group.Instances))

	instanceIds := make([]*string, 0, len(group.Instances))
	for _, i := range group.Instances {
		instanceIds = append(instanceIds, aws.String(*i.InstanceId))
	}
	input := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{{
			Name:   aws.String("instance-id"),
			Values: instanceIds,
		}},
	}
	err := ec2Client.DescribeInstancesPages(input,
		func(output *ec2.DescribeInstancesOutput, isLast bool) bool {
			for _, r := range output.Reservations {
				for _, i := range r.Instances {
					instanceGroups = append(instanceGroups, &InstanceGroup{instance: i, group: group})
				}
			}
			return !isLast
		})
	if err != nil {
		return nil, err
	}
	return instanceGroups, nil
}

func DescribeAutoScalingGroup(asgClient *autoscaling.AutoScaling, ec2Client *ec2.EC2, name string) (InstanceGroups, error) {
	group, err := getAutoScalingGroup(asgClient, name)
	if err != nil {
		return nil, err
	}
	return GetInstancesForGroup(ec2Client, group)
}

func DescribeInstanceByInternalDNS(
	ec2Client *ec2.EC2,
	asgClient *autoscaling.AutoScaling,
	instanceInternalDNS string,
) (*InstanceGroup, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{{
			Name:   aws.String("network-interface.private-dns-name"),
			Values: []*string{aws.String(instanceInternalDNS)},
		}},
	}
	var instance *ec2.Instance
	err := ec2Client.DescribeInstancesPages(input,
		func(output *ec2.DescribeInstancesOutput, isLast bool) bool {
			instance = output.Reservations[0].Instances[0]
			return false
		})
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("%s: No matching instance could be found", instanceInternalDNS)
	}

	log.Printf("Internal DNS '%s' is instance ID '%s'", instanceInternalDNS, *instance.InstanceId)

	var groupName string
	asgInput := &autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []*string{aws.String(*instance.InstanceId)},
	}
	err = asgClient.DescribeAutoScalingInstancesPages(asgInput,
		func(output *autoscaling.DescribeAutoScalingInstancesOutput, isLast bool) bool {
			for _, instance := range output.AutoScalingInstances {
				groupName = *(instance.AutoScalingGroupName)
				return false
			}
			return true
		})
	if err != nil {
		return nil, err
	}
	if groupName == "" {
		return nil, fmt.Errorf("%s: No matching ASG could be found", instanceInternalDNS)
	}

	group, err := getAutoScalingGroup(asgClient, groupName)
	if err != nil {
		return nil, err
	}

	return &InstanceGroup{instance: instance, group: group}, nil
}

func GetAllAutoScalingGroups(client *autoscaling.AutoScaling) ([]*autoscaling.Group, error) {
	var groups []*autoscaling.Group
	in := &autoscaling.DescribeAutoScalingGroupsInput{
		MaxRecords: aws.Int64(100),
	}
	err := client.DescribeAutoScalingGroupsPages(in,
		func(page *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
			groups = append(groups, page.AutoScalingGroups...)
			return !lastPage
		})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func getAutoScalingGroup(client *autoscaling.AutoScaling, name string) (*autoscaling.Group, error) {
	in := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{name}),
		MaxRecords:            aws.Int64(1),
	}
	out, err := client.DescribeAutoScalingGroups(in)
	if err != nil {
		return nil, err
	}
	if len(out.AutoScalingGroups) != 1 {
		return nil, fmt.Errorf("expected exactly 1 ASG description for '%s' got %d", name, len(out.AutoScalingGroups))
	}
	return out.AutoScalingGroups[0], nil
}

func DetachInstance(client *autoscaling.AutoScaling, groupId, id string, removeNode bool) error {
	log.Printf("Detaching instance '%s' from ASG '%s'...", id, groupId)
	in := &autoscaling.DetachInstancesInput{
		InstanceIds:                    aws.StringSlice([]string{id}),
		AutoScalingGroupName:           aws.String(groupId),
		ShouldDecrementDesiredCapacity: aws.Bool(removeNode),
	}
	_, err := client.DetachInstances(in)
	if err != nil {
		return err
	}
	log.Printf("Instance '%s' detached.", id)
	return nil
}

func TerminateInstanceByID(client *ec2.EC2, id string) error {
	log.Printf("Terminating instance '%s'...", id)
	in := &ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{id}),
	}
	_, err := client.TerminateInstances(in)
	if err != nil {
		return err
	}
	log.Printf("Instance '%s' succesfully terminated.", id)
	return nil
}

func GetEKSCluserByName(client *eks.EKS, name string) (*eks.Cluster, error) {
	cluserInput := &eks.DescribeClusterInput{
		Name: aws.String(name),
	}
	clusterDesc, err := client.DescribeCluster(cluserInput)
	if err != nil {
		return nil, err
	}
	return clusterDesc.Cluster, nil
}

func getK8sConfigForCluster(cluster *eks.Cluster) (*rest.Config, error) {
	gen, err := token.NewGenerator(true, false)
	if err != nil {
		return nil, err
	}
	opts := &token.GetTokenOptions{ClusterID: aws.StringValue(cluster.Name)}
	tok, err := gen.GetWithOptions(opts)
	if err != nil {
		return nil, err
	}
	ca, err := base64.StdEncoding.DecodeString(aws.StringValue(cluster.CertificateAuthority.Data))
	if err != nil {
		return nil, err
	}

	return &rest.Config{
		Host:        aws.StringValue(cluster.Endpoint),
		BearerToken: tok.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: ca,
		},
	}, nil
}

func GetEKSCluserByURL(client *eks.EKS, url string) (*eks.Cluster, error) {
	listOutput, err := client.ListClusters(nil)
	if err != nil {
		return nil, err
	}
	for _, clusterName := range listOutput.Clusters {
		cluster, err := GetEKSCluserByName(client, *clusterName)
		if err != nil {
			return nil, err
		}
		if *cluster.Endpoint == url {
			return cluster, nil
		}
	}
	return nil, fmt.Errorf("unable to find cluster with URL %s", url)
}

func GetK8sConfigByClusterName(client *eks.EKS, clusterName string) (*rest.Config, error) {
	cluster, err := GetEKSCluserByName(client, clusterName)
	if err != nil {
		return nil, err
	}
	return getK8sConfigForCluster(cluster)
}
