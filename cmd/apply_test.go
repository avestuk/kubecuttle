package cmd

import "testing"

func TestApplyCmd(t *testing.T) {
	input := `
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
	applyCmd.Run(rootCmd, []string{"-f", input})
}
