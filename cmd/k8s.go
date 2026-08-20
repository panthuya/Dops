package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig string
	namespace  string
	exactMatch bool

	searchHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
	resTypeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Bold(true).Width(15)
	resNameStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
	nsStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	secretKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Bold(true)
	secretValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	boxStyle       = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4ADE80")).
			Padding(0, 1).
			MarginLeft(4).
			MarginTop(0).
			MarginBottom(1).
			Width(100) // Forces wrapping for extremely long secret strings like certificates
)

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Kubernetes search operations",
}

var validResources = []string{"configmaps", "cronjobs", "deployments", "statefulsets", "services", "secrets", "all"}

var k8sHierarchy = map[string][]string{
	"root": {"apiVersion", "kind", "metadata", "spec", "data", "stringData", "type", "clusters", "contexts", "users", "preferences", "current-context", "secrets", "imagePullSecrets", "rules", "subjects", "roleRef", "webhooks"},
	"metadata": {"name", "namespace", "labels", "annotations", "finalizers", "ownerReferences"},
	"spec": {"replicas", "selector", "template", "containers", "initContainers", "volumes", "serviceAccountName", "ports", "type", "rules", "tls", "clusterIP", "sessionAffinity", "strategy", "minReadySeconds", "nodeSelector", "affinity", "tolerations"},
	"selector": {"matchLabels", "matchExpressions"},
	"template": {"metadata", "spec"},
	"containers": {"name", "image", "ports", "env", "envFrom", "resources", "volumeMounts", "livenessProbe", "readinessProbe", "securityContext", "command", "args", "imagePullPolicy", "workingDir"},
	"initContainers": {"name", "image", "ports", "env", "envFrom", "resources", "volumeMounts", "securityContext", "command", "args", "imagePullPolicy", "workingDir"},
	"ports": {"name", "containerPort", "nodePort", "targetPort", "port", "protocol"},
	"env": {"name", "value", "valueFrom"},
	"volumeMounts": {"name", "mountPath", "subPath", "readOnly"},
	"volumes": {"name", "configMap", "secret", "emptyDir", "persistentVolumeClaim", "hostPath", "projected"},
	"rules": {"host", "http"},
	"http": {"paths"},
	"paths": {"path", "pathType", "backend"},
	"backend": {"service"},
	"service": {"name", "port"},
	"tls": {"hosts", "secretName"},
	"clusters": {"cluster", "name"},
	"contexts": {"context", "name"},
	"users": {"user", "name"},
	"cluster": {"server", "certificate-authority-data", "insecure-skip-tls-verify"},
	"context": {"cluster", "user", "namespace"},
	"user": {"token", "client-certificate-data", "client-key-data", "username", "password", "exec", "auth-provider"},
}

func sanitizeK8sYAML(data string) string {
	lines := strings.Split(data, "\n")
	var out []string

	stack := []string{"root"}
	indentStack := []int{0}

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			out = append(out, line)
			continue
		}

		if trim == "---" {
			stack = []string{"root"}
			indentStack = []int{0}
			out = append(out, trim)
			continue
		}

		isListItem := strings.HasPrefix(trim, "- ")
		keyPart := trim
		if isListItem {
			keyPart = strings.TrimPrefix(keyPart, "- ")
		}
		
		colonIdx := strings.Index(keyPart, ":")
		key := keyPart
		if colonIdx != -1 {
			key = strings.TrimSpace(keyPart[:colonIdx])
		}

		bestIndent := -1
		for i := len(stack) - 1; i >= 0; i-- {
			parent := stack[i]
			allowedChildren := k8sHierarchy[parent]
			found := false
			for _, child := range allowedChildren {
				if child == key {
					found = true
					break
				}
			}
			if found {
				bestIndent = indentStack[i]
				if parent != "root" {
					bestIndent += 2
				}
                
				stack = stack[:i+1]
				indentStack = indentStack[:i+1]
				break
			}
		}

		if bestIndent == -1 {
            if len(indentStack) > 0 {
			    bestIndent = indentStack[len(indentStack)-1] + 2
            } else {
                bestIndent = 0
            }
		}

		isBlockOpener := strings.HasSuffix(trim, ":")

		leadingSpaces := bestIndent
		if isListItem {
			leadingSpaces -= 2
			if leadingSpaces < 0 {
				leadingSpaces = 0
			}
		}

		outLine := strings.Repeat(" ", leadingSpaces) + trim
		out = append(out, outLine)

		if isBlockOpener {
			stack = append(stack, key)
			if isListItem {
				indentStack = append(indentStack, leadingSpaces+2)
			} else {
				indentStack = append(indentStack, leadingSpaces)
			}
		}
	}
	return strings.Join(out, "\n")
}

var fmtCmd = &cobra.Command{
	Use:   "fmt [file.yaml]",
	Short: "Auto-format and fix indentation in Kubernetes YAML files",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		filename := args[0]
		data, err := os.ReadFile(filename)
		if err != nil {
			fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error reading file: %v\n", err)))
			return
		}

		// Run heuristic auto-fixer first to correct severely broken structure
		sanitized := sanitizeK8sYAML(string(data))

		dec := yaml.NewDecoder(strings.NewReader(sanitized))
		
		var out bytes.Buffer
		enc := yaml.NewEncoder(&out)
		enc.SetIndent(2)

		for {
			var node yaml.Node
			err := dec.Decode(&node)
			if err != nil {
				if err == io.EOF {
					break
				}
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error parsing YAML: %v\n", err)))
				fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("Note: The YAML indentation is too broken to auto-fix. Please check line structure."))
				return
			}
			err = enc.Encode(&node)
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error formatting YAML: %v\n", err)))
				return
			}
		}
		enc.Close()

		err = os.WriteFile(filename, out.Bytes(), 0644)
		if err != nil {
			fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("❌ Error writing file: %v\n", err)))
			return
		}
		
		fmt.Printf(" ✔ %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80")).Bold(true).Render("Successfully formatted: " + filename))
	},
}

var searchCmd = &cobra.Command{
	Use:   "search [resource_type] [keyword]",
	Short: "Search k8s resources (e.g. configmaps, secrets) for a keyword",
	Long:  "Resource type can be: configmaps, cronjobs, deployments, statefulsets, services, secrets, or all. Example: dops k8s search secrets my-password",
	Args:  cobra.RangeArgs(0, 2),
	ValidArgs: validResources,
	Run: func(cmd *cobra.Command, args []string) {
		resType := "all"
		keyword := ""

		if len(args) == 1 {
			argLow := strings.ToLower(args[0])
			isRes := false
			for _, r := range validResources {
				if strings.HasPrefix(r, argLow) {
					isRes = true
					break
				}
			}
			if argLow == "cm" || argLow == "cj" || argLow == "deploy" || argLow == "sts" || argLow == "svc" {
				isRes = true
			}

			if isRes {
				resType = argLow
				keyword = ""
			} else {
				keyword = args[0]
			}
		} else if len(args) >= 2 {
			resType = strings.ToLower(args[0])
			keyword = args[1]
		}

		if !exactMatch {
			keyword = strings.ToLower(keyword)
		}

		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		if kubeconfig != "" {
			loadingRules.ExplicitPath = kubeconfig
		}
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		
		config, err := kubeConfig.ClientConfig()
		if err != nil {
			fmt.Printf("Error building kubeconfig: %v\n", err)
			return
		}

		config.WarningHandler = rest.NoWarnings{}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			fmt.Printf("Error creating clientset: %v\n", err)
			return
		}

		ctx := context.TODO()
		fmt.Printf("\n%s\n", searchHeaderStyle.Render(fmt.Sprintf("🔎 Searching for %q in %s", keyword, resType)))
		if namespace != "" {
			fmt.Printf("%s\n\n", nsStyle.Render(fmt.Sprintf("   Namespace: %s", namespace)))
		} else {
			fmt.Printf("%s\n\n", nsStyle.Render("   Namespace: all"))
		}
		
		foundCount := 0
		var tbRows [][]string

		matches := func(name string) bool {
			if exactMatch {
				return strings.Contains(name, keyword)
			}
			return strings.Contains(strings.ToLower(name), keyword)
		}

		addMatchRow := func(kind, ns, name, usedBy, details string) {
			foundCount++
			if details != "" {
				details = lipgloss.NewStyle().Width(80).Render(details)
			}
			tbRows = append(tbRows, []string{
				resTypeStyle.Render(kind),
				nsStyle.Render(ns) + "/" + resNameStyle.Render(name),
				usedBy,
				details,
			})
		}

		findUsages := func(ns, name string, isSecret bool) []string {
			var usages []string
			if deps, err := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{}); err == nil {
				for _, d := range deps.Items {
					used := false
					for _, v := range d.Spec.Template.Spec.Volumes {
						if isSecret && v.Secret != nil && v.Secret.SecretName == name { used = true }
						if !isSecret && v.ConfigMap != nil && v.ConfigMap.Name == name { used = true }
					}
					for _, c := range d.Spec.Template.Spec.Containers {
						for _, env := range c.EnvFrom {
							if isSecret && env.SecretRef != nil && env.SecretRef.Name == name { used = true }
							if !isSecret && env.ConfigMapRef != nil && env.ConfigMapRef.Name == name { used = true }
						}
						for _, env := range c.Env {
							if env.ValueFrom != nil {
								if isSecret && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == name { used = true }
								if !isSecret && env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name == name { used = true }
							}
						}
					}
					if used { usages = append(usages, "Deployment/"+d.Name) }
				}
			}
			if sts, err := clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{}); err == nil {
				for _, s := range sts.Items {
					used := false
					for _, v := range s.Spec.Template.Spec.Volumes {
						if isSecret && v.Secret != nil && v.Secret.SecretName == name { used = true }
						if !isSecret && v.ConfigMap != nil && v.ConfigMap.Name == name { used = true }
					}
					for _, c := range s.Spec.Template.Spec.Containers {
						for _, env := range c.EnvFrom {
							if isSecret && env.SecretRef != nil && env.SecretRef.Name == name { used = true }
							if !isSecret && env.ConfigMapRef != nil && env.ConfigMapRef.Name == name { used = true }
						}
						for _, env := range c.Env {
							if env.ValueFrom != nil {
								if isSecret && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == name { used = true }
								if !isSecret && env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name == name { used = true }
							}
						}
					}
					if used { usages = append(usages, "StatefulSet/"+s.Name) }
				}
			}
			return usages
		}

		checkType := func(t string) bool {
			return resType == "all" || strings.HasPrefix(t, resType) || strings.HasPrefix(resType, t)
		}

		// ConfigMaps
		if checkType("configmap") || checkType("cm") {
			cms, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing ConfigMaps: %v\n", err)))
			} else {
				for _, item := range cms.Items {
					matchFound := matches(item.Name)
					if !matchFound {
						for k, v := range item.Data {
							if matches(k) || matches(v) {
								matchFound = true
								break
							}
						}
					}
					if matchFound {
						usedBy := ""
						if usages := findUsages(item.Namespace, item.Name, false); len(usages) > 0 {
							usedBy = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("Used by: ") + strings.Join(usages, ", ")
						}
						addMatchRow("ConfigMap", item.Namespace, item.Name, usedBy, "")
					}
				}
			}
		}

		// CronJobs
		if checkType("cronjob") || checkType("cj") {
			cjs, err := clientset.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing CronJobs: %v\n", err)))
			} else {
				for _, item := range cjs.Items {
					if matches(item.Name) {
						addMatchRow("CronJob", item.Namespace, item.Name, "", "")
					}
				}
			}
		}

		// Deployments
		if checkType("deployment") || checkType("deploy") {
			deps, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing Deployments: %v\n", err)))
			} else {
				for _, item := range deps.Items {
					if matches(item.Name) {
						addMatchRow("Deployment", item.Namespace, item.Name, "", "")
					}
				}
			}
		}

		// StatefulSets
		if checkType("statefulset") || checkType("sts") {
			sts, err := clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing StatefulSets: %v\n", err)))
			} else {
				for _, item := range sts.Items {
					if matches(item.Name) {
						addMatchRow("StatefulSet", item.Namespace, item.Name, "", "")
					}
				}
			}
		}

		// Services
		if checkType("service") || checkType("svc") {
			svcs, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing Services: %v\n", err)))
			} else {
				for _, item := range svcs.Items {
					if matches(item.Name) {
						addMatchRow("Service", item.Namespace, item.Name, "", "")
					}
				}
			}
		}

		// Secrets
		if checkType("secret") {
			secrets, err := clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				fmt.Printf(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(fmt.Sprintf("   [!] Error listing Secrets: %v\n", err)))
			} else {
				for _, item := range secrets.Items {
					// Ignore Helm release secrets (massive binary blobs that clutter the terminal)
					if item.Type == "helm.sh/release.v1" {
						continue
					}

					matchFound := matches(item.Name)
					if !matchFound {
						for k, v := range item.Data {
							if matches(k) || matches(string(v)) {
								matchFound = true
								break
							}
						}
					}

					if matchFound {
						usedBy := ""
						if usages := findUsages(item.Namespace, item.Name, true); len(usages) > 0 {
							usedBy = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("Used by: ") + strings.Join(usages, ", ")
						}
						var lines []string
						for k, v := range item.Data {
							lines = append(lines, fmt.Sprintf("%s: %s", secretKeyStyle.Render(k), secretValStyle.Render(string(v))))
						}
						details := ""
						if len(lines) > 0 {
							details = lipgloss.JoinVertical(lipgloss.Left, lines...)
						}
						addMatchRow("Secret", item.Namespace, item.Name, usedBy, details)
					}
				}
			}
		}

		if foundCount == 0 {
			fmt.Println(nsStyle.Render("   No resources found."))
		} else {
			t := table.New().
				Border(lipgloss.RoundedBorder()).
				BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#4ADE80"))).
				Headers("TYPE", "NAMESPACE/NAME", "USED BY", "DETAILS").
				Rows(tbRows...)
			
			fmt.Println("\n" + t.Render())
			fmt.Printf("\n   %s %d resources.\n\n", searchHeaderStyle.Render("Total Found:"), foundCount)
		}
	},
}

func init() {
	rootCmd.AddCommand(k8sCmd)
	k8sCmd.AddCommand(searchCmd)
	k8sCmd.AddCommand(fmtCmd)

	k8sCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", "", "absolute path to the kubeconfig file")
	k8sCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "namespace to search (default all)")
	searchCmd.Flags().BoolVarP(&exactMatch, "exact", "e", false, "Exact case matching")
}
