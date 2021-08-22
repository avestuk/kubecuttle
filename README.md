# kubecuttle

## Usage

```bash
go build

# Set the KUBECONFIG envvar to point to your kube config
export KUBECONFIG=~/.kube/config.yaml

# Use kubecuttle
cat <<EOF | ./kubecuttle apply -f -
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

cat <<EOF | ./kubecuttle apply -f -
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

# To run tests
go test -v ./...
```

## Notes
Used Cobra Generator to generate app
* Ease of use
* Cobra pretty standard in Go so others could extend it easily. 

Uses Server Side Apply in order to deal with arbitrary resource Kinds. Downside
of this is that there's no validation client side and you have to have a K8s API
server to talk to in order to run tests.

## Aim

## The challenge

The goal is to create a CLI (called `kubecuttle`) which reimplements a small subset of `kubectl apply` functionality. 

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