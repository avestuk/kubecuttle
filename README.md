# kubecuttle

## Notes

Used Cobra Generator to generate app
* Ease of use
* Cobra pretty standard in Go so people can easily navigate it. 


## Aim

## The challenge

The goal is to create a CLI (called `kubecuttle`) which reimplements a small subset of `kubectl apply` functionality. Choose any language you like, we will find a reviewer who is versed in it. We recommend you spend approximately 6 hours on this exercise.

## Requirements

The following command should successfully deploy the busybox pods to a cluster (you can assume that the `sre-test` namespace exists):

```bash
cat <<EOF | kubecuttle apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: busybox-sleep
  namespace: sre-test
spec:
  containers:
  - name: busybox
    image: busybox
    args:
    - sleep
    - "1000000"
---
apiVersion: v1
kind: Pod
metadata:
  name: busybox-sleep-less
  namespace: sre-test
spec:
  containers:
  - name: busybox
    image: busybox
    args:
    - sleep
    - "1000"
EOF
```

The following command you can run subsequently should successfully update already deployed busybox pod with updated labels:

```bash
cat <<EOF | kubecuttle apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: busybox-sleep
  namespace: sre-test
  labels:
    foo: bar
spec:
  containers:
  - name: busybox
    image: busybox
    args:
    - sleep
    - "1000000"
EOF
```

A successful submission, will:
1. Work with a kubernetes cluster that is currently active as a context.
2. Report any problems with the input.
3. Be designed with the intention of extending it in the future to support other `kinds`.
4. Contain a short documentation on how we can test the CLI.
5. Describe what steps would be taken if you were to release this software to a wider audience (from the non-functional perspective).

## Running Kubernetes locally

If you don't have your own local cluster already, you might find these local k8s distros useful:
1. [kind](https://kind.sigs.k8s.io/)
2. [docker desktop](https://birthday.play-with-docker.com/kubernetes-docker-desktop/)

## Submitting your solution

The full code of your solution should be available as a Github repo.
