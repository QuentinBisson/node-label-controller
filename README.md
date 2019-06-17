# node-label-controller

This repository implements a simple controller for watching Node resources and sets the `kubermatic.io/uses-container-linux` label to true if the node is operating Container Linux.

This controller was build using the sample controller at its source. This allowed me to learn about the internals of the kubernetes API and how to build controller from scratch

## Details

The node-label-controller uses [client-go library](https://github.com/kubernetes/client-go/tree/master/tools/cache) extensively.

## Running

```sh
# assumes you have a working kubeconfig, not required if operating in-cluster
cd node-label-controller
go build -o node-label-controller .
./node-label-controller -kubeconfig=$HOME/.kube/config

# Deploy the operator in its namespace with rbac
kubectl apply -f config/deploy/rbac
kubectl apply -f config/deploy

# check nodes operating container linux were updated
kubectl get nodes
```

## Cleanup

You can clean up the created controller with:

```sh
kubectl delete -f config/deploy
```

## Ideas for the future

- Try to implement it using kubebuilder, kudos or operator sdk
- Add label to other nodes for different oses
- Unit test it