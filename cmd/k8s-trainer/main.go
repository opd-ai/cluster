// cmd/k8s-trainer wraps each training stage as a Kubernetes Job and streams
// its logs back to the caller.
//
// It generates a Job manifest from a template, applies it with kubectl, waits
// for completion, streams pod logs to stdout, and cleans up.  GPU resource
// requests are injected from the namespace config.  Jobs are scheduled onto
// trainer-tainted nodes via nodeSelector and tolerations.
//
// Usage:
//
//	k8s-trainer [flags] <namespace> <stage>
//
// Stage is one of: train-ns, train-repo, convert.
//
// Flags:
//
//	-namespaces    path to namespaces.yaml (default: configs/namespaces.yaml)
//	-repo          repo label (required for stage=train-repo)
//	-kubeconfig    path to kubeconfig (default: cluster/kubeconfig)
//	-image         trainer Docker image (default: ghcr.io/opd-ai/cluster-trainer:latest)
//	-gpu           number of GPUs per pod (default: 1)
//	-timeout       job timeout in minutes (default: 120)
//	-cleanup       delete Job after completion (default: true)
//	-datasets      base dataset path as a volume (default: /mnt/warm/datasets)
//	-checkpoints   base checkpoint path as a volume (default: /mnt/warm/checkpoints)
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// jobSpec holds template data for a Kubernetes Job manifest.
type jobSpec struct {
	Name        string
	Namespace   string // k8s namespace (always "default")
	Stage       string
	NSName      string // pipeline namespace
	Repo        string
	Image       string
	GPUCount    int
	DatasetPath string
	CkptPath    string
	NSFilePath  string
}

// jobTemplate is the Job manifest template.
var jobTemplate = template.Must(template.New("job").Parse(`apiVersion: batch/v1
kind: Job
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app: cluster-trainer
    pipeline-ns: "{{.NSName}}"
    stage: "{{.Stage}}"
spec:
  backoffLimit: 1
  template:
    spec:
      restartPolicy: Never
      tolerations:
        - key: workload
          operator: Equal
          value: trainer
          effect: NoSchedule
      nodeSelector:
        role: trainer
      containers:
        - name: trainer
          image: {{.Image}}
          command: ["python3", "/app/python/train.py"]
          args:
            - "--mode"
            - "{{.Stage}}"
            - "--namespace"
            - "{{.NSName}}"
{{- if .Repo}}
            - "--repo"
            - "{{.Repo}}"
{{- end}}
            - "--namespaces"
            - "/config/namespaces.yaml"
            - "--dataset-dir"
            - "/data/datasets/{{.NSName}}"
            - "--output-dir"
            - "/data/checkpoints/{{.NSName}}"
          resources:
            limits:
              nvidia.com/gpu: "{{.GPUCount}}"
          volumeMounts:
            - name: data
              mountPath: /data
            - name: config
              mountPath: /config
      volumes:
        - name: data
          hostPath:
            path: {{.DatasetPath}}
        - name: config
          configMap:
            name: cluster-namespaces
`))

func main() {
	namespacesPath := flag.String("namespaces", "configs/namespaces.yaml", "Path to namespaces.yaml")
	repo := flag.String("repo", "", "Repo label (required for train-repo)")
	kubeconfig := flag.String("kubeconfig", "cluster/kubeconfig", "Path to kubeconfig")
	image := flag.String("image", "ghcr.io/opd-ai/cluster-trainer:latest", "Trainer image")
	gpu := flag.Int("gpu", 1, "GPUs per pod")
	timeout := flag.Int("timeout", 120, "Job timeout (minutes)")
	cleanup := flag.Bool("cleanup", true, "Delete Job after completion")
	datasets := flag.String("datasets", "/mnt/warm/datasets", "Dataset volume path")
	checkpoints := flag.String("checkpoints", "/mnt/warm/checkpoints", "Checkpoint volume path")
	flag.Parse()

	if flag.NArg() < 2 {
		flag.Usage()
		log.Fatal("usage: k8s-trainer [flags] <pipeline-namespace> <stage>")
	}
	nsName := flag.Arg(0)
	stage := flag.Arg(1)

	validStages := map[string]bool{"train-ns": true, "train-repo": true, "convert": true}
	if !validStages[stage] {
		log.Fatalf("invalid stage %q; must be one of: train-ns, train-repo, convert", stage)
	}
	if stage == "train-repo" && *repo == "" {
		log.Fatal("-repo is required for stage=train-repo")
	}

	jobName := fmt.Sprintf("cluster-train-%s-%s-%d", nsName, stage, time.Now().Unix())
	if *repo != "" {
		jobName = fmt.Sprintf("cluster-train-%s-%s-%s-%d", nsName, stage, *repo, time.Now().Unix())
	}

	spec := jobSpec{
		Name:        jobName,
		Namespace:   "default",
		Stage:       stage,
		NSName:      nsName,
		Repo:        *repo,
		Image:       *image,
		GPUCount:    *gpu,
		DatasetPath: *datasets,
		CkptPath:    *checkpoints,
		NSFilePath:  *namespacesPath,
	}

	// Write Job manifest to a temp file.
	tmpFile := filepath.Join(os.TempDir(), jobName+".yaml")
	if err := writeJobManifest(tmpFile, spec); err != nil {
		log.Fatalf("write job manifest: %v", err)
	}
	defer os.Remove(tmpFile)

	env := append(os.Environ(), "KUBECONFIG="+*kubeconfig)

	// Apply the Job.
	if err := kubectlRun(env, "apply", "-f", tmpFile); err != nil {
		log.Fatalf("kubectl apply: %v", err)
	}

	log.Printf("Job %s submitted; waiting (timeout: %d min)...", jobName, *timeout)

	// Wait for job completion.
	deadline := time.Now().Add(time.Duration(*timeout) * time.Minute)
	for time.Now().Before(deadline) {
		out, err := kubectlOutput(env, "get", "job", jobName,
			"-o", "jsonpath={.status.conditions[?(@.type=='Complete')].status}")
		if err == nil && strings.TrimSpace(out) == "True" {
			log.Printf("Job %s completed successfully.", jobName)
			break
		}
		failed, _ := kubectlOutput(env, "get", "job", jobName,
			"-o", "jsonpath={.status.conditions[?(@.type=='Failed')].status}")
		if strings.TrimSpace(failed) == "True" {
			streamLogs(env, jobName)
			log.Fatalf("Job %s failed.", jobName)
		}
		time.Sleep(15 * time.Second)
	}

	// Stream final logs.
	streamLogs(env, jobName)

	// Cleanup.
	if *cleanup {
		if err := kubectlRun(env, "delete", "job", jobName); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}
}

// writeJobManifest renders the Job template to a file.
func writeJobManifest(path string, spec jobSpec) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jobTemplate.Execute(f, spec)
}

// kubectlRun runs kubectl with the given args, streaming output to os.Stdout.
func kubectlRun(env []string, args ...string) error {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// kubectlOutput runs kubectl and returns its stdout as a string.
func kubectlOutput(env []string, args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}

// streamLogs fetches logs from the first pod of the Job.
func streamLogs(env []string, jobName string) {
	podName, err := kubectlOutput(env,
		"get", "pods",
		"-l", "job-name="+jobName,
		"-o", "jsonpath={.items[0].metadata.name}",
	)
	if err != nil || strings.TrimSpace(podName) == "" {
		log.Printf("no pods found for job %s", jobName)
		return
	}
	_ = kubectlRun(env, "logs", strings.TrimSpace(podName))
}
