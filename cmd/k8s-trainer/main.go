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
	"regexp"
	"strings"
	"text/template"
	"time"
)

// jobSpec holds template data for a Kubernetes Job manifest.
type jobSpec struct {
	Name        string
	Namespace   string // k8s namespace (always "default")
	Stage       string
	Mode        string // mapped mode value for train.py ("namespace" or "repo")
	NSName      string // pipeline namespace
	Repo        string
	Image       string
	GPUCount    int
	DatasetPath string
	CkptPath    string
}

// stageToMode maps k8s-trainer stage names to train.py --mode values.
var stageToMode = map[string]string{
	"train-ns":   "namespace",
	"train-repo": "repo",
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
{{- if eq .Stage "convert"}}
          command: ["python3", "/app/tools/llama.cpp/convert_lora_to_gguf.py"]
          args:
            - "--input"
            - "/data/checkpoints/{{.NSName}}"
            - "--output"
            - "/data/checkpoints/{{.NSName}}/model.gguf"
{{- else}}
          command: ["python3", "/app/python/train.py"]
          args:
            - "--mode"
            - "{{.Mode}}"
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
{{- end}}
          resources:
            limits:
              nvidia.com/gpu: "{{.GPUCount}}"
          volumeMounts:
            - name: datasets
              mountPath: /data/datasets
            - name: checkpoints
              mountPath: /data/checkpoints
            - name: config
              mountPath: /config
      volumes:
        - name: datasets
          hostPath:
            path: {{.DatasetPath}}
        - name: checkpoints
          hostPath:
            path: {{.CkptPath}}
        - name: config
          configMap:
            name: cluster-namespaces
`))

func main() {
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

	// Validate nsName and repo to prevent argument injection in kubectl commands.
	if !isValidK8sName(nsName) {
		log.Fatalf("invalid namespace name %q: contains disallowed characters", nsName)
	}
	if *repo != "" && !isValidK8sName(*repo) {
		log.Fatalf("invalid repo name %q: contains disallowed characters", *repo)
	}

	jobName := fmt.Sprintf("cluster-train-%s-%s-%d", nsName, stage, time.Now().Unix())
	if *repo != "" {
		jobName = fmt.Sprintf("cluster-train-%s-%s-%s-%d", nsName, stage, *repo, time.Now().Unix())
	}

	spec := jobSpec{
		Name:        jobName,
		Namespace:   "default",
		Stage:       stage,
		Mode:        stageToMode[stage],
		NSName:      nsName,
		Repo:        *repo,
		Image:       *image,
		GPUCount:    *gpu,
		DatasetPath: *datasets,
		CkptPath:    *checkpoints,
	}

	// Write Job manifest to a temp file with secure random name.
	f, err := os.CreateTemp("", jobName+"-*.yaml")
	if err != nil {
		log.Fatalf("create temp file: %v", err)
	}
	tmpFile := f.Name()
	f.Close()
	
	if err := writeJobManifest(tmpFile, spec); err != nil {
		log.Fatalf("write job manifest: %v", err)
	}
	defer os.Remove(tmpFile)

	env := append(os.Environ(), "KUBECONFIG="+*kubeconfig)

	// Apply the Job.
	if err := kubectlRun(env, "apply", "-f", tmpFile); err != nil {
		log.Fatalf("kubectl apply: %v", err)
	}

	// Defer cleanup to ensure it runs even if job fails.
	if *cleanup {
		defer func() {
			if err := kubectlRun(env, "delete", "job", jobName); err != nil {
				log.Printf("cleanup: %v", err)
			}
		}()
	}

	log.Printf("Job %s submitted; waiting (timeout: %d min)...", jobName, *timeout)

	// Wait for job completion.
	deadline := time.Now().Add(time.Duration(*timeout) * time.Minute)
	completed := false
	for time.Now().Before(deadline) {
		out, err := kubectlOutput(env, "get", "job", jobName,
			"-o", "jsonpath={.status.conditions[?(@.type=='Complete')].status}")
		if err == nil && strings.TrimSpace(out) == "True" {
			log.Printf("Job %s completed successfully.", jobName)
			completed = true
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
	if !completed {
		streamLogs(env, jobName)
		log.Fatalf("Job %s did not complete within %d minute(s) deadline.", jobName, *timeout)
	}

	// Stream final logs.
	streamLogs(env, jobName)
}

// safeLabel matches Kubernetes label-safe identifiers and DNS subdomain
// components: alphanumeric, dashes, dots, and forward slashes.
var safeLabel = regexp.MustCompile(`^[a-zA-Z0-9._/:-]+$`)

// validateJobSpec returns an error if any jobSpec string field contains
// characters that could break YAML structure or inject arbitrary YAML/shell.
func validateJobSpec(spec jobSpec) error {
	fields := map[string]string{
		"Name":      spec.Name,
		"Namespace": spec.Namespace,
		"Stage":     spec.Stage,
		"Mode":      spec.Mode,
		"NSName":    spec.NSName,
		"Repo":      spec.Repo,
		"Image":     spec.Image,
	}
	for name, val := range fields {
		if val == "" {
			continue
		}
		if !safeLabel.MatchString(val) {
			return fmt.Errorf("jobSpec field %s contains unsafe characters: %q", name, val)
		}
	}
	return nil
}

// writeJobManifest renders the Job template to a file.
func writeJobManifest(path string, spec jobSpec) error {
	if err := validateJobSpec(spec); err != nil {
		return err
	}
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
	if err := kubectlRun(env, "logs", strings.TrimSpace(podName)); err != nil {
		log.Printf("warning: stream logs for %s: %v", jobName, err)
	}
}

// isValidK8sName validates that a string is a safe Kubernetes name.
// Kubernetes names must match [a-z0-9]([-a-z0-9]*[a-z0-9])? per DNS-1123.
func isValidK8sName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	re := regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	return re.MatchString(s)
}
