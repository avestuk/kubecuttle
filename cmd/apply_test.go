package cmd

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var onePod = `
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
`

var onePodUpdate = `
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
`

var twoPods = `
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
`

func TestApplyCmd(t *testing.T) {

	//applyCmd.Run(rootCmd, []string{"-f", input})
}

func TestDecodePod(t *testing.T) {
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePod)), 4096)

	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")
	require.Len(t, pods, 1)

}
func TestDecodePods(t *testing.T) {
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)

	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")
	require.Len(t, pods, 2)
}

func TestCreateOrApplyPods(t *testing.T) {
	client, err := clientInit()
	require.NoError(t, err, "failed to initialize client")

	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePod)), 4096)
	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := client.CoreV1().Pods(pods[0].Namespace).Delete(ctx, pods[0].Name, *v1.NewDeleteOptions(0))
		require.NoErrorf(t, err, "failed to delete pod: %s/%s", pods[0].Namespace, pods[0].Name)
	})

	err = CreateOrApplyPod(client, pods[0])
	require.NoError(t, err, "failed to create or apply pods")

	yamlDecoder = yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePodUpdate)), 4096)
	updatedPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	err = CreateOrApplyPod(client, updatedPods[0])
	require.NoError(t, err, "failed to create or apply pods")

}

func TestApplyPods(t *testing.T) {
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	for _, pod := range pods {
		ApplyPod(nil, pod)
	}
}
