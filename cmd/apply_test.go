package cmd

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestApplyPods(t *testing.T) {
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	ApplyPods(nil, pods)
}
