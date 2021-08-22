package cmd

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
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
    - "1000000"
---
apiVersion: v1
kind: Pod
metadata:
  name: busybox-sleep
  namespace: sre-test
spec:
  containers:
  - name: busybox
    image: busybox:stable
    args:
    - sleep
    - "1000000"
`
var onePodInvalidSpecUpdate = `
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
  name: busybox-sleep
  namespace: sre-test
spec:
  containers:
  - name: busybox
    image: busybox
    args:
    - sleep
    - "300"
    env:
    - name: foo
      value: bar
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
		Kind        string
		Version     string
	}{
		{
			onePod,
			1,
			"Pod",
			"v1",
		},
		{
			twoPods,
			2,
			"Pod",
			"v1",
		},
	}

	for _, tt := range cases {
		objects, err := decodeInput([]byte(tt.Input))
		require.NoError(t, err, "failed to decode objects")
		require.Len(t, objects, tt.ObjectCount, "expected two objects")

		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		obj := &unstructured.Unstructured{}
		runtimeObj, _, err := decodeRawObjects(decodingSerializer, objects[0].Raw, obj)
		require.NoError(t, err, "failed to decode raw objects")
		gvk := runtimeObj.GetObjectKind().GroupVersionKind()
		require.NotNil(t, gvk)
		require.Equal(t, tt.Kind, gvk.Kind)
		require.Equal(t, tt.Version, gvk.Version)
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
	objects, err := decodeInput([]byte(incorrectSpec))
	require.NoError(t, err, "got error from DecodePods")

	decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	obj := &unstructured.Unstructured{}
	runtimeObj, gvk, err := decodeRawObjects(decodingSerializer, objects[0].Raw, obj)
	require.NoError(t, err, "failed to decode raw objects")

	gvr, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	require.NoError(t, err, "failed to get gvr")

	// Obtain the REST interface for the GVR and set a namespace if the K8s
	// kind is namespaced. E.g. pod vs PV
	var dr dynamic.ResourceInterface
	if gvr.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = dynamicClient.Resource(gvr.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = dynamicClient.Resource(gvr.Resource)
	}

	// Marshal Object into JSON
	data, err := json.Marshal(runtimeObj)
	require.NoError(t, err, "failed to marshal json to runtime obj")

	// Apply the thing
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	obj, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "kubecuttle",
	})
	require.Error(t, err, "expected error applying incorrect object spec, got obj:\n%v", obj)
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

func TestApply(t *testing.T) {
	cases := []struct {
		Name         string
		Input        string
		ApplySuccess bool
	}{
		{
			"one pod",
			onePod,
			true,
		},
		{
			"two pods",
			twoPods,
			true,
		},
		{
			"incorrect spec",
			incorrectSpec,
			false,
		},
	}

	for _, tt := range cases {
		// Build required k8s clients
		client, dynamicClient, err := buildK8sClients()
		require.NoError(t, err, "failed to build client")

		// Return GroupMappings for K8s API resources.
		gr, err := restmapper.GetAPIGroupResources(client.Discovery())
		require.NoError(t, err, "failed to get API group resources")
		mapper := restmapper.NewDiscoveryRESTMapper(gr)

		// Build decoder
		objects, err := decodeInput([]byte(tt.Input))
		require.NoError(t, err, "failed to decode test input, %s", tt.Name)

		// Create a serializer that can decode
		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

		for _, object := range objects {
			obj := &unstructured.Unstructured{}

			// Decode the object into a k8s runtime Object. This also
			// returns the GroupValueKind for the object. GVK identifies a
			// kind. A kind is the implementation of a K8s API resource.
			// For instance, a pod is a resource and it's v1/Pod
			// implementation is its kind.
			runtimeObj, gvk, err := decodeRawObjects(decodingSerializer, object.Raw, obj)
			require.NoError(t, err, "failed to decode object")

			// Find the resource mapping for the GVK extracted from the
			// object. A resource type is uniquely identified by a Group,
			// Version, Resource tuple where a kind is identified by a
			// Group, Version, Kind tuple. You can see these mappings using
			// kubectl api-resources.
			gvr, err := getResourceMapping(mapper, gvk)
			require.NoError(t, err, "failed to get GroupVersionResource from GroupVersionKind")

			// Establish a REST mapping for the GVR. For instance
			// for a Pod the endpoint we need is: GET /apis/v1/namespaces/{namespace}/pods/{name}
			// As some objects are not namespaced (e.g. PVs) a namespace may not be required.
			dr := getRESTMapping(dynamicClient, gvr.Scope.Name(), obj.GetNamespace(), gvr.Resource)

			// Marshall our runtime object into json. All json is
			// valid yaml but not all yaml is valid json. The
			// APIServer works on json.
			data, err := marshallRuntimeObj(runtimeObj)
			require.NoError(t, err, "failed to marshal json to runtime obj")

			// Attempt to ServerSideApply the provided object.
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				dr.Delete(ctx, obj.GetName(), *metav1.NewDeleteOptions(0))
			}()

			_, err = applyObjects(dr, obj, data)
			switch {
			case tt.ApplySuccess:
				require.NoError(t, err, "failed to patch object, test: %s", tt.Name)
			case !tt.ApplySuccess:
				require.Error(t, err, "expected failure to patch object but got none, test: %s", tt.Name)
			}
		}
	}
}

func TestUpdate(t *testing.T) {
	cases := []struct {
		Name          string
		Input         string
		UpdateSuccess bool
	}{
		{
			"meta update",
			onePodMetaUpdate,
			true,
		},
		{
			"spec update",
			onePodSpecUpdate,
			true,
		},
		{
			"invalid spec update",
			onePodInvalidSpecUpdate,
			false,
		},
	}

	for i, tt := range cases {
		// Build required k8s clients
		client, dynamicClient, err := buildK8sClients()
		require.NoError(t, err, "failed to build client")

		// Return GroupMappings for K8s API resources.
		gr, err := restmapper.GetAPIGroupResources(client.Discovery())
		require.NoError(t, err, "failed to get API group resources")
		mapper := restmapper.NewDiscoveryRESTMapper(gr)

		// Build decoder
		objects, err := decodeInput([]byte(tt.Input))
		require.NoError(t, err, "failed to decode test input, %s", tt.Name)

		// Create a serializer that can decode
		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

		for _, object := range objects {
			obj := &unstructured.Unstructured{}

			// Decode the object into a k8s runtime Object. This also
			// returns the GroupValueKind for the object. GVK identifies a
			// kind. A kind is the implementation of a K8s API resource.
			// For instance, a pod is a resource and it's v1/Pod
			// implementation is its kind.
			runtimeObj, gvk, err := decodeRawObjects(decodingSerializer, object.Raw, obj)
			require.NoError(t, err, "failed to decode object")

			// Find the resource mapping for the GVK extracted from the
			// object. A resource type is uniquely identified by a Group,
			// Version, Resource tuple where a kind is identified by a
			// Group, Version, Kind tuple. You can see these mappings using
			// kubectl api-resources.
			gvr, err := getResourceMapping(mapper, gvk)
			require.NoError(t, err, "failed to get GroupVersionResource from GroupVersionKind")

			// Establish a REST mapping for the GVR. For instance
			// for a Pod the endpoint we need is: GET /apis/v1/namespaces/{namespace}/pods/{name}
			// As some objects are not namespaced (e.g. PVs) a namespace may not be required.
			dr := getRESTMapping(dynamicClient, gvr.Scope.Name(), obj.GetNamespace(), gvr.Resource)

			// Marshall our runtime object into json. All json is
			// valid yaml but not all yaml is valid json. The
			// APIServer works on json.
			data, err := marshallRuntimeObj(runtimeObj)
			require.NoError(t, err, "failed to marshal json to runtime obj")

			// Attempt to ServerSideApply the provided object.
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				dr.Delete(ctx, obj.GetName(), *metav1.NewDeleteOptions(0))
			}()

			_, err = applyObjects(dr, obj, data)
			switch i {
			// First object will be apply creation
			case 0:
				require.NoError(t, err, "failed to patch object, test: %s", tt.Name)
			// Second object will be apply update
			case 1:
				if tt.UpdateSuccess {
					require.NoError(t, err, "failed to update object, test; %s", tt.Name)
				} else {
					require.Error(t, err, "expected failure to patch object but got none, test: %s", tt.Name)
				}
			}
		}
	}
}
