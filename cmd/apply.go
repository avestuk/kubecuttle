package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"

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
		client, err := clientInit()
		if err != nil {
			return fmt.Errorf("got an error: %s", err)
		}

		// kubecuttle apply arg1 -f arg2
		// This would print arg1
		fmt.Printf("apply called with args: %s\n", args)

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
		yamlDecoder := yaml.NewYAMLOrJSONDecoder(io.NopCloser(strings.NewReader(string(fileContents))), 4096)

		pods, err := DecodePods(yamlDecoder)
		if err != nil {
			return fmt.Errorf("failed to decode input, got err: %w", err)
		}

		for _, pod := range pods {
			if err := CreateOrApplyPod(client, pod); err != nil {
				fmt.Printf("got err creating or applying pods, err: %s", err)
			}
		}

		fmt.Printf("apply called with flag arguments: %s\n", input)

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

func clientInit() (*kubernetes.Clientset, error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("KUBECONFIG was empty")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read kubeconfig from file: %s", kubeconfigPath)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s client, got err: %w", err)
	}

	return client, nil
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

func CreateOrApplyPod(client *kubernetes.Clientset, pod *v1.Pod) error {
	// Attempt to get pods. If pods cannot be gotten then create them.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := client.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Set Managed Field to be our CLI App
			pod.SetManagedFields([]metav1.ManagedFieldsEntry{
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
			pod, err := client.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
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
	return ApplyPod(client, p)

}

func ApplyPod(client *kubernetes.Clientset, pod *v1.Pod) error {
	podApplyConf, err := applyv1.ExtractPod(pod, fieldManager)
	if err != nil {
		return fmt.Errorf("got err extracting pod, err: %w", err)
	}

	fmt.Printf("%v", *podApplyConf)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	appliedPod, err := client.CoreV1().Pods(pod.Namespace).Apply(ctx, podApplyConf, metav1.ApplyOptions{
		FieldManager: fieldManager,
	})

	if err != nil {
		return fmt.Errorf("got err applying pod, err: %w", err)
	}

	fmt.Printf("applied pod: \n%v", appliedPod)

	return nil
}
