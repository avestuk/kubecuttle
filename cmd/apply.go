package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"time"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

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

		objects, err := decode(yamlDecoder)
		if err != nil {
			return fmt.Errorf("failed to decode objects, got err: %w", err)
		}

		// Build clients
		client, dynamicClient, err := buildK8sClients()
		if err != nil {
			return fmt.Errorf("failed to build clients, got err: %w", err)
		}

		// Fetch K8s group resources
		gr, err := restmapper.GetAPIGroupResources(client.Discovery())
		if err != nil {
			return fmt.Errorf("failed to get API group resources, got err: %w", err)
		}
		mapper := restmapper.NewDiscoveryRESTMapper(gr)

		decodingSerializer := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

		for _, object := range objects {
			obj := &unstructured.Unstructured{}

			runtimeObj, gvk, err := decodingSerializer.Decode(object.Raw, nil, obj)
			if err != nil {
				return fmt.Errorf("failed to decode object, got err: %w", err)
			}

			gvr, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if err != nil {
				return fmt.Errorf("failed to get gvr, got err: %w", err)
			}

			// 6. Obtain the REST interface for the GVR
			var dr dynamic.ResourceInterface
			if gvr.Scope.Name() == meta.RESTScopeNameNamespace {
				dr = dynamicClient.Resource(gvr.Resource).Namespace(obj.GetNamespace())
			} else {
				dr = dynamicClient.Resource(gvr.Resource)
			}

			// 6. Marshal Object into JSON
			data, err := json.Marshal(runtimeObj)
			if err != nil {
				return fmt.Errorf("failed to marshal json to runtime obj, got err: %w", err)
			}

			// 7. Apply the thing
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

// DecodePods decodes yaml -> *v1.Pod.
func DecodePods(y *yaml.YAMLOrJSONDecoder) ([]*v1.Pod, error) {
	pods := []*v1.Pod{}

	for {
		pod := &v1.Pod{}
		if err := y.Decode(pod); err != nil {
			// We expect an EOF error when decoding is done,
			// anything else should count as a function fail.
			if err.Error() != "EOF" {
				return nil, err
			}

			return pods, nil
		}

		pods = append(pods, pod)
	}
}

func decode(y *yaml.YAMLOrJSONDecoder) ([]*runtime.RawExtension, error) {
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

func applyObjects() {

}

func CreateOrApplyPod(client *kubernetes.Clientset, desiredPod *v1.Pod) error {
	// Attempt to get pods. If pods cannot be gotten then create them.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	existingPod, err := client.CoreV1().Pods(desiredPod.Namespace).Get(ctx, desiredPod.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Set Managed Field to be our CLI App
			desiredPod.SetManagedFields([]metav1.ManagedFieldsEntry{
				{
					Manager:   fieldManager,
					Operation: metav1.ManagedFieldsOperationApply,
					//FieldsV1: &metav1.FieldsV1{
					//	Raw: []byte("f:metadata"),
					//},
				},
			})
			// TODO Call CreatePod func
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pod, err := client.CoreV1().Pods(desiredPod.Namespace).Create(ctx, desiredPod, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			fmt.Printf("pod %s/%s created", pod.Namespace, pod.Name)
			return nil
		} else {
			return err
		}
	}

	// Call ApplyPod func
	return ApplyPod(client, existingPod, desiredPod)

}

func ApplyPod(client *kubernetes.Clientset, existingPod, desiredPod *v1.Pod) error {
	podApplyConf, err := applyv1.ExtractPod(existingPod, fieldManager)
	if err != nil {
		return fmt.Errorf("got err extracting pod, err: %w", err)
	}

	diffMetadata(podApplyConf, existingPod, desiredPod)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	appliedPod, err := client.CoreV1().Pods(existingPod.Namespace).Apply(ctx, podApplyConf, metav1.ApplyOptions{
		FieldManager: fieldManager,
	})

	if err != nil {
		return fmt.Errorf("got err applying pod, err: %w", err)
	}

	fmt.Printf("applied pod: \n%v", appliedPod)

	return nil
}

// diffMetadata diffs the existing vs the desired Pods ObjectMeta as these are
// the only fields that can be changed at runtime.
func diffMetadata(podApplyConf *applyv1.PodApplyConfiguration, existingPod, desiredPod *v1.Pod) {
	// TODO WithOwnerReference
	if !reflect.DeepEqual(existingPod.ObjectMeta.Annotations, desiredPod.ObjectMeta.Annotations) {
		podApplyConf.WithAnnotations(desiredPod.Annotations)
	}
	if !reflect.DeepEqual(existingPod.ObjectMeta.Labels, desiredPod.ObjectMeta.Labels) {
		podApplyConf.WithLabels(desiredPod.Labels)
	}
	if !reflect.DeepEqual(existingPod.ObjectMeta.Finalizers, desiredPod.ObjectMeta.Finalizers) {
		podApplyConf.WithFinalizers(desiredPod.GetFinalizers()...)
	}
}

func diffSpec(podApplyConf *applyv1.PodApplyConfiguration, existingPod, desiredPod *v1.Pod) error {
	if !reflect.DeepEqual(existingPod.Spec, desiredPod.Spec) {
		return fmt.Errorf("existing and desired pods have different specs. spec cannot be updated at runtime")
	}
	return nil
}
