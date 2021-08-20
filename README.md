# rotate-eks-asg

Rolling Cluster Node Upgrades for AWS EKS

## Use Case

Apply security fixes, rollout new Kubernetes versions, or replace faulty nodes on AWS.

In general terms:

- You run Kubernetes via [AWS EKS](https://aws.amazon.com/eks/)
- Your cluster is made up of [EC2 Auto Scaling Groups (ASG)](https://docs.aws.amazon.com/autoscaling/ec2/userguide/AutoScalingGroup.html)
- You want to replace one or all nodes in those ASGs (e.g. to [activate a new launch configuration](https://docs.aws.amazon.com/autoscaling/ec2/userguide/LaunchConfiguration.html))
- The replacement has to be done gracefully, node-by-node, and respects [availability constraints in your cluster](https://kubernetes.io/docs/tasks/run-application/configure-pdb/)

## Building

```
make build
```

Binaries are placed in the `bin/` directory.

## Usage

See `./bin/rotate-eks-asg --help` for full usage.

Prior to running, you must be logged into the AWS account where the target cluster lives.

To rotate all nodes in a cluster, run:
```
rotate-eks-asg --cluster my-cluster
```

Omiting the `--cluster` parameter will rotate whichever cluster your current kubeconfig is pointed to.
Pass `--dryrun` to display what _would_ be rotated without actually performing the rotation.
Pass `--limit n` to limit the number of nodes to be rotated. Nodes are rotated in order of oldest to newest, so to rotate out the oldest node, run:
```
rotate-eks-asg --cluster my-cluster --limit 1
```

### Makefile

You must have a valid kubeconfig and be logged into AWS cli on the same account as the kubernetes cluster your current context is pointed to. 

The Makefile includes targets for running the rotator. This is equivilant to running the rotator without the `--cluster` parameter.

```
# Rotate all nodes in the cluster
make rotate

# Rotate only the oldest node
make rotate-oldest

# Pass the DRYRUN flag to perform a dry run (and print what would be rotated)
make -e DRYRUN=1 rotate
```
