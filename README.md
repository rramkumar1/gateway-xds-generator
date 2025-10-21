This is a very simple tool that allows you to visualize the Envoy configuration
for a Gateway resource.

No need to run a controller from any particular vendor. Just point at a cluster
and run it.

## How to use

Ensure your kubeconfig is pointed at a k8s cluster with Gateway API resources
deployed.

Then run:

```go run main.go --gateway foo-gateway --namespace bar```

This will dump out the Envoy XDS config to a file in the same directory.

## Use cases

* Learning - This is an easy way to get familiar with Envoy configs and how they
  map to Gateway API

* Quick POC - Added a new feature? Quickly see how it maps to Envoy.


Credit to https://github.com/kubernetes-sigs/cloud-provider-kind for the
translation code
