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
	"k8s.io/apimachinery/pkg/runtime/schema"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
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

var incorrectSpec = `
apiVersion: v1
kind: Pod
metadata:
  name: busybox-wrong
  namespace: sre-test
spec:
  containers:
  - name: busybox
    image: busybox
    args:
    - sleep
    - "300"
should:
  not:
  - be
  - here
`

func TestDecode(t *testing.T) {
	cases := []struct {
		Input       string
		ObjectCount int
	}{
		{onePod, 1},
		{twoPods, 2},
	}

	for _, tt := range cases {
		yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(tt.Input)), 4096)

		objects, err := decodeInput(yamlDecoder)
		require.NoError(t, err, "failed to decode objects")
		require.Len(t, objects, tt.ObjectCount, "expected two objects")

		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		obj := &unstructured.Unstructured{}
		runtimeObj, _, err := decodeRawObjects(decodingSerializer, objects[0].Raw, obj)
		require.NoError(t, err, "failed to decode raw objects")
		gvk := runtimeObj.GetObjectKind().GroupVersionKind()
		require.NotNil(t, gvk)
		require.Equal(t, "Pod", gvk.Kind)
		require.Equal(t, gvk.Version, "v1")
	}
}

func TestIncorrectSpec(t *testing.T) {
	// Build required k8s clients
	client, dynamicClient, err := buildK8sClients()
	require.NoError(t, err, "failed to build client")

	// Return GroupMappings for K8s API resources.
	gr, err := restmapper.GetAPIGroupResources(client.Discovery())
	require.NoError(t, err, "failed to get API group resources")
	mapper := restmapper.NewDiscoveryRESTMapper(gr)

	// Build decoder
	yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(incorrectSpec)), 4096)

	objects, err := decodeInput(yamlDecoder)
	require.NoError(t, err, "got error from DecodePods")

	decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	runtimeObj, gvk, err := decodeRawObjects(decodingSerializer, objects[0].Raw, obj)
	require.NoError(t, err, "failed to decode raw objects")

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
}

func TestBuildClients(t *testing.T) {
	client, dClient, err := buildK8sClients()
	require.NoError(t, err, "failed to build k8s clients")

	// Get pods from kube-system to prove client works
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pods, err := client.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "failed to get pods")
	require.Greater(t, len(pods.Items), 1, "expected kube-system to return more than 1 pod")

	// Get pods from kube-system to prove the dynamic client works
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dpods, err := dClient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}).Namespace("kube-system").List(ctx, metav1.ListOptions{})
	require.NoError(t, err, "failed to get pods")
	require.Greater(t, len(dpods.Items), 1, "expected kube-system to return more than 1 pod")
}

func TestThing(t *testing.T) {
	// Build required k8s clients
	client, dynamicClient, err := buildK8sClients()
	require.NoError(t, err, "failed to build client")

	// Return GroupMappings for K8s API resources.
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
