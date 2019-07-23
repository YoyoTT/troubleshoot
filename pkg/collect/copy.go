package collect

import (
	"bytes"
	"encoding/json"
	"fmt"

	troubleshootv1beta1 "github.com/replicatedhq/troubleshoot/pkg/apis/troubleshoot/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type CopyOutput struct {
	Files  map[string][]byte `json:"copy/,omitempty"`
	Errors map[string][]byte `json:"copy-errors/,omitempty"`
}

func Copy(copyCollector *troubleshootv1beta1.Copy, redact bool) error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	copyOutput := &CopyOutput{
		Files:  make(map[string][]byte),
		Errors: make(map[string][]byte),
	}

	pods, podsErrors := listPodsInSelectors(client, copyCollector.Namespace, copyCollector.Selector)
	if len(podsErrors) > 0 {
		errorBytes, err := marshalNonNil(podsErrors)
		if err != nil {
			return err
		}
		copyOutput.Errors[getCopyErrosFileName(copyCollector)] = errorBytes
	}

	if len(pods) > 0 {
		for _, pod := range pods {
			files, copyErrors := copyFiles(client, pod, copyCollector)
			if len(copyErrors) > 0 {
				key := fmt.Sprintf("%s/%s/%s-errors.json", pod.Namespace, pod.Name, copyCollector.ContainerPath)
				copyOutput.Errors[key], err = marshalNonNil(copyErrors)
				if err != nil {
					return err
				}
				continue
			}

			for k, v := range files {
				copyOutput.Files[k] = v
			}
		}

		if redact {
			// TODO
		}
	}

	b, err := json.MarshalIndent(copyOutput, "", "  ")
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", b)

	return nil
}

func copyFiles(client *kubernetes.Clientset, pod corev1.Pod, copyCollector *troubleshootv1beta1.Copy) (map[string][]byte, map[string]string) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, map[string]string{"error": err.Error()}
	}

	container := pod.Spec.Containers[0].Name
	if copyCollector.ContainerName != "" {
		container = copyCollector.ContainerName
	}

	command := []string{"cat", copyCollector.ContainerPath}

	req := client.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec")
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, map[string]string{"error": err.Error()}
	}

	parameterCodec := runtime.NewParameterCodec(scheme)
	req.VersionedParams(&corev1.PodExecOptions{
		Command:   command,
		Container: container,
		Stdin:     true,
		Stdout:    false,
		Stderr:    true,
		TTY:       false,
	}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, map[string]string{"error": err.Error()}
	}

	output := new(bytes.Buffer)
	var stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: output,
		Stderr: &stderr,
		Tty:    false,
	})
	if err != nil {
		return nil, map[string]string{
			"stdout": output.String(),
			"stderr": stderr.String(),
			"error":  err.Error(),
		}
	}

	return map[string][]byte{
		fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, copyCollector.ContainerPath): output.Bytes(),
	}, nil
}

func getCopyErrosFileName(copyCollector *troubleshootv1beta1.Copy) string {
	if len(copyCollector.CollectorName) > 0 {
		return fmt.Sprintf("%s.json", copyCollector.CollectorName)
	}
	// TODO: random part
	return "errors.json"
}
