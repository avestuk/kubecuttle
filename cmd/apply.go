package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	"k8s.io/apimachinery/pkg/runtime/schema"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	fieldManager string = "kubecuttle"
)

// applyCmd represents the Apply command
var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		input, err := cmd.Flags().GetString("file")
		if err != nil {
			return fmt.Errorf("could not get value of file flag, got err: %s", err)
		}

		var (
			fileContents []byte
			file         string
		)

		// Parse input
		switch input {
		case "-":
			file = "/dev/stdin"
		default:
			file = input
		}

		fileContents, err = ioutil.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %s, got err: %s", file, err)
		}

		// Attempt to decode input into pods.
		yamlDecoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(fileContents), 4096)

		objects, err := decodeInput(yamlDecoder)
		if err != nil {
			return fmt.Errorf("failed to decode objects, got err: %w", err)
		}

		// Build clients
		client, dynamicClient, err := buildK8sClients()
		if err != nil {
			return fmt.Errorf("failed to build clients, got err: %w", err)
		}

		// Fetch K8s group resources, essentially a list of resources
		// and their mapping to a Kubernetes Kind. Essentially equates
		// to kubectl api-resources.
		gr, err := restmapper.GetAPIGroupResources(client.Discovery())
		if err != nil {
			return fmt.Errorf("failed to get API group resources, got err: %w", err)
		}
		mapper := restmapper.NewDiscoveryRESTMapper(gr)

		// Create a serializer that can decode
		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

		for _, object := range objects {
			obj := &unstructured.Unstructured{}

			// Decode the object into a k8s runtime Object. This also
			// returns the GroupValueKind for the object. GVK identifies a
			// kind. A kind is the implementation of a K8s API resource.
			// For instance, a pod is a resource and it's v1/Pod
			// implementation is its kind.
			// TODO: Understand if this is needed.
			runtimeObj, gvk, err := decodingSerializer.Decode(object.Raw, nil, obj)
			if err != nil {
				return fmt.Errorf("failed to decode object, got err: %w", err)
			}

			// Find the resource mapping for the GVK extracted from the
			// object. A resource type is uniquely identified by a Group,
			// Version, Resource tuple where a kind is identified by a
			// Group, Version, Kind tuple. You can see these mappings using
			// kubectl api-resources.
			gvr, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if err != nil {
				return fmt.Errorf("failed to get gvr, got err: %w", err)
			}

			// Establish a REST mapping for the GVR. For instance
			// for a Pod the endpoint we need is: GET /apis/v1/namespaces/{namespace}/pods/{name}
			// As some objects are not namespaced (e.g. PVs) a namespace may not be required.
			var dr dynamic.ResourceInterface
			if gvr.Scope.Name() == meta.RESTScopeNameNamespace {
				dr = dynamicClient.Resource(gvr.Resource).Namespace(obj.GetNamespace())
			} else {
				dr = dynamicClient.Resource(gvr.Resource)
			}

			// Marshall our runtime object into json. All json is
			// valid yaml but not all yaml is valid json. The
			// APIServer works on json.
			data, err := json.Marshal(runtimeObj)
			if err != nil {
				return fmt.Errorf("failed to marshal json to runtime obj, got err: %w", err)
			}

			// Attempt to ServerSideApply the provided object.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			k8sObj, err := dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "kubecuttle",
			})
			if err != nil {
				return fmt.Errorf("failed to apply obj, got err: %w", err)
			}

			fmt.Printf("\n%s %s/%s updated\n", k8sObj.GetKind(), k8sObj.GetNamespace(), k8sObj.GetName())
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(applyCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// ApplyCmd.PersistentFlags().String("foo", "", "A help for foo")
	applyCmd.PersistentFlags().StringP("file", "f", "", "pass - to apply yaml configuration from STDIN")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// ApplyCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// buildK8sClients returns a typed K8s client and a dynamic k8s client.
func buildK8sClients() (*kubernetes.Clientset, dynamic.Interface, error) {
	config, err := buildConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build config, got err: %w", err)
	}

	client, err := typedClientInit(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build k8s client, got err: %w", err)
	}

	dynamicClient, err := dynamicClientInit(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build dynmaic client, got err: %w", err)
	}

	return client, dynamicClient, nil
}

func typedClientInit(config *rest.Config) (*kubernetes.Clientset, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s client, got err: %w", err)
	}

	return client, nil
}

func dynamicClientInit(config *rest.Config) (dynamic.Interface, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return dynamicClient, nil
}

func buildConfig() (*rest.Config, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("KUBECONFIG was empty")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig from file: %s", kubeconfigPath)
	}

	return config, nil
}

func decodeInput(y *yaml.YAMLOrJSONDecoder) ([]*runtime.RawExtension, error) {
	objects := []*runtime.RawExtension{}

	for {
		obj := &runtime.RawExtension{}
		if err := y.Decode(obj); err != nil {
			// We expect an EOF error when decoding is done,
			// anything else should count as a function fail.
			if err.Error() != "EOF" {
				return nil, err
			}
			return objects, nil
		}
		objects = append(objects, obj)
	}
}

func decodeRawObjects(decoder runtime.Serializer, data []byte, into *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind, error) {
	return decoder.Decode(data, nil, into)
}

func getResourceMapping(mapper meta.RESTMapper, gvk *schema.GroupVersionKind) (*meta.RESTMapping, error) {
	return mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

func getRESTMapping(dynamicClient dynamic.Interface, name meta.RESTScopeName, namespace string, resource schema.GroupVersionResource) dynamic.ResourceInterface {
	var dr dynamic.ResourceInterface
	if name == meta.RESTScopeNameNamespace {
		dr = dynamicClient.Resource(resource).Namespace(namespace)
	} else {
		dr = dynamicClient.Resource(resource)
	}

	return dr
}

func marshallRuntimeObj(obj runtime.Object) ([]byte, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json to runtime obj, got err: %w", err)
	}

	return data, nil
}

func applyObjects() {

}
