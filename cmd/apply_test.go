package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
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

var onePodMetaUpdate = `
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

var onePodSpecUpdate = `
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
    - "300"
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

func TestThing(t *testing.T) {
	// 1. Have GVK need GVR
	//dynamicClient, discoveryClient, err := dynamicClientInit()
	dynamicClient, _, err := dynamicClientInit()
	require.NoError(t, err, "failed to build client")
	//mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	// TODO - Check whether you can just build a regular client and not do the whole MemCache thing
	client, err := typedClientInit()
	require.NoError(t, err, "failed to build client")
	gr, err := restmapper.GetAPIGroupResources(client.Discovery())
	require.NoError(t, err, "failed to get API group resources")
	mapper := restmapper.NewDiscoveryRESTMapper(gr)

	// 3. Decode YAML
	decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}

	// 4. Find GVK
	runtimeObj, gvk, err := decodingSerializer.Decode([]byte(onePod), nil, obj)
	require.NoError(t, err, "got err deserializng")

	fmt.Printf("gvk: %s", gvk.String())

	// 4. Find GVR
	gvr, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	require.NoError(t, err, "failed to get gvr")

	// 6. Obtain the REST interface for the GVR
	var dr dynamic.ResourceInterface
	if gvr.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = dynamicClient.Resource(gvr.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = dynamicClient.Resource(gvr.Resource)
	}

	// 6. Marshal Object into JSON
	data, err := json.Marshal(runtimeObj)
	require.NoError(t, err, "failed to marshal json to runtime obj")

	// 7. Apply the thing
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	obj, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubecuttle",
	})
	require.NoError(t, err, "failed to apply obj")

	// Go again with the next pod
	modifiedObj := &unstructured.Unstructured{}

	// 4. Find GVK
	mruntimeObj, gvk, err := decodingSerializer.Decode([]byte(onePodMetaUpdate), nil, modifiedObj)
	require.NoError(t, err, "got err deserializng")

	// 4. Find GVR
	gvr, err = mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	require.NoError(t, err, "failed to get gvr")

	// 6. Obtain the REST interface for the GVR
	if gvr.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = dynamicClient.Resource(gvr.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = dynamicClient.Resource(gvr.Resource)
	}

	// 6. Marshal Object into JSON
	mdata, err := json.Marshal(mruntimeObj)
	require.NoError(t, err, "failed to marshal json to runtime obj")

	// 7. Apply the thing
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = dr.Patch(ctx, modifiedObj.GetName(), types.ApplyPatchType, mdata, metav1.PatchOptions{
		FieldManager: "kubecuttle",
	})
	require.NoError(t, err, "failed to apply obj")

}

func TestCreateOrApplyPods(t *testing.T) {
	client, err := typedClientInit()
	require.NoError(t, err, "failed to initialize client")

	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePod)), 4096)
	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := client.CoreV1().Pods(pods[0].Namespace).Delete(ctx, pods[0].Name, *metav1.NewDeleteOptions(0))
		require.NoErrorf(t, err, "failed to delete pod: %s/%s", pods[0].Namespace, pods[0].Name)
	})

	err = CreateOrApplyPod(client, pods[0])
	require.NoError(t, err, "failed to create or apply pods")

	yamlDecoder = yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePodMetaUpdate)), 4096)
	updatedPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	err = CreateOrApplyPod(client, updatedPods[0])
	require.NoError(t, err, "failed to create or apply pods")

}

func TestApplyPods(t *testing.T) {
	client, err := typedClientInit()
	require.NoError(t, err, "failed to initialize client")

	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	existingPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	yamlDecoder = yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	desiredPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	err = ApplyPod(client, existingPods[0], desiredPods[0])
	require.NoError(t, err, "failed to apply pods")
}

func TestPodsSpecUpdate(t *testing.T) {
	client, err := typedClientInit()
	require.NoError(t, err, "failed to initialize client")

	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePod)), 4096)
	pods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := client.CoreV1().Pods(pods[0].Namespace).Delete(ctx, pods[0].Name, *metav1.NewDeleteOptions(0))
		require.NoErrorf(t, err, "failed to delete pod: %s/%s", pods[0].Namespace, pods[0].Name)
	})

	err = CreateOrApplyPod(client, pods[0])
	require.NoError(t, err, "failed to create or apply pods")

	yamlDecoder = yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(onePodSpecUpdate)), 4096)
	updatedPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	err = CreateOrApplyPod(client, updatedPods[0])
	require.NoError(t, err, "failed to create or apply pods")
}

func TestDiffMetadata(t *testing.T) {
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	existingPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	yamlDecoder = yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(twoPods)), 4096)
	desiredPods, err := DecodePods(yamlDecoder)
	require.NoError(t, err, "got err from decode pods")

	podApplyConf, err := applyv1.ExtractPod(existingPods[0], fieldManager)
	require.NoError(t, err, "failed to extract apply config")

	diffMetadata(podApplyConf, existingPods[0], desiredPods[0])
	require.Equal(t, podApplyConf.Labels, desiredPods[0].Labels)
	require.Equal(t, podApplyConf.Annotations, desiredPods[0].Annotations)
	require.Equal(t, podApplyConf.Finalizers, desiredPods[0].Finalizers)
}
