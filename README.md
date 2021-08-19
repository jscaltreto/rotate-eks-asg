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

You must have a valid kubeconfig and be logged into AWS cli on the same account as the kubernetes cluster your current context is pointed to. 

The Makefile includes targets for running the rotator.

```
# Rotate all nodes in the cluster
make rotate

# Rotate only the oldest node
make rotate-oldest

# Pass the DRYRUN flag to perform a dry run (and print what would be rotated)
make -e DRYRUN=1 rotate
```

The rotator may also be run manually. See `./bin/rotate-eks-asg --help` for usage.
