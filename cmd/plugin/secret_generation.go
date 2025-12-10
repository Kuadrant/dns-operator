package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"github.com/kuadrant/dns-operator/cmd/plugin/output"
)

const kubectl = "kubectl"

type GenerateSecretFlags struct {
	contextName    string
	name           string
	namespace      string
	serviceAccount string
	dirty          bool
}

var generateSecretFlags = GenerateSecretFlags{
	dirty: false,
}

var addClusterSecretCMD = &cobra.Command{
	Use:   "add-cluster-secret",
	RunE:  addClusterSecret,
	Short: "Create a kubeconfig secret",
}

type Secret struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   Metadata          `yaml:"metadata"`
	StringData map[string]string `yaml:"stringData"`
}

type Metadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

// Config represents a Kubernetes configuration file.
type Config struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Clusters       []NamedCluster `yaml:"clusters"`
	Contexts       []NamedContext `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
	Users          []NamedUser    `yaml:"users"`
}

// NamedCluster holds the name and cluster details.
type NamedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

// Cluster holds the server and certificate authority data.
type Cluster struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data"`
}

// NamedContext holds the name and context details.
type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

// Context holds the cluster and user names.
type Context struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

// NamedUser holds the name and user details.
type NamedUser struct {
	Name string `yaml:"name"`
	User User   `yaml:"user"`
}

// User holds the user's token.
type User struct {
	Token string `yaml:"token"`
}

func init() {
	addClusterSecretCMD.Flags().StringVarP(&generateSecretFlags.contextName, "context", "c", "", "kubeconfig context for the secondry cluster (required)")
	if err := addClusterSecretCMD.MarkFlagRequired("context"); err != nil {
		panic(err)
	}

	addClusterSecretCMD.Flags().StringVar(&generateSecretFlags.name, "name", "", "name for the secret (defaults to context name)")
	addClusterSecretCMD.Flags().StringVarP(&generateSecretFlags.namespace, "namespace", "n", "dns-operator-system", "namespace to create the secret in")
	addClusterSecretCMD.Flags().StringVarP(&generateSecretFlags.serviceAccount, "service-account", "a", "dns-operator-remote-cluster", "service account name to use")
}

func addClusterSecret(_ *cobra.Command, _ []string) error {
	dirtyStr := os.Getenv("KUBECTL_DNS_DIRTY")
	if strings.Compare(strings.ToLower(dirtyStr), "true") == 0 {
		output.Formatter.Debug("kubectl-kuadrant_dns running in dirty mode")
		generateSecretFlags.dirty = true
	}

	output.Formatter.Debug(fmt.Sprintf("Set the secret name to context name if not set; context: %s; name: %s", generateSecretFlags.contextName, generateSecretFlags.name))
	if len(generateSecretFlags.name) == 0 {
		generateSecretFlags.name = generateSecretFlags.contextName
	}

	err := checkKubectl()
	if err != nil {
		return err
	}

	clusterCA, err := getClusterCA()
	if err != nil {
		return err
	}

	clusterServer, err := getClusterServer()
	if err != nil {
		return err
	}

	serviceAccountToken, err := generateServiceAccountToken()
	if err != nil {
		return err
	}

	output.Formatter.Debug("creation directory to store temporary files")
	tmpDir, err := os.MkdirTemp("", "kudectl-dev")
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(tmpDir); err != nil {
			output.Formatter.Error(err, "deferred call failed")
		}
	}()

	kubeConfig := defineKubeConfig(clusterServer, clusterCA, serviceAccountToken)
	kubeConfigFile, err := saveKubeConfig(kubeConfig, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(kubeConfigFile.Name()); err != nil {
			output.Formatter.Error(err, "deferred call failed")
		}
	}()

	err = verifyKubeConfigConnection(kubeConfigFile.Name())
	if err != nil {
		return err
	}

	secret, err := generateSecret(kubeConfig)
	if err != nil {
		return err
	}

	secretFile, err := saveSecret(*secret, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(secretFile.Name()); err != nil {
			output.Formatter.Error(err, "deferred call failed")
		}
	}()

	err = applySecretToCluser(secretFile.Name())
	if err != nil {
		return err
	}

	output.Formatter.Info("The operator should now be able to discover, and connect to this cluster")

	return nil
}

func getClusterCA() (string, error) {
	output.Formatter.Debug("Get the cluster CA certificate from the remote cluster")
	args := []string{
		fmt.Sprintf("--context=%v", generateSecretFlags.contextName),
		"config",
		"view",
		"--raw",
		"--minify",
		"--flatten",
		"-o",
		"jsonpath='{.clusters[].cluster.certificate-authority-data}'",
	}
	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	clusterCARaw, err := cmd.Output()
	if err != nil {
		output.Formatter.Error(err, fmt.Sprintf("unable to get certificate authority from kubeconfig; context: %s; stdErr: %s; stdOut: %s", generateSecretFlags.contextName, stderr.String(), cmd.Stdout))
		return "", fmt.Errorf("unable to get certificate for %v from kubeconfig", generateSecretFlags.contextName)
	}
	clusterCA := string(clusterCARaw)
	clusterCA = strings.Trim(clusterCA, "'")
	return clusterCA, nil

}

func getClusterServer() (string, error) {
	output.Formatter.Debug("Get the cluster server URL from the remote cluster")
	args := []string{
		fmt.Sprintf("--context=%v", generateSecretFlags.contextName),
		"config",
		"view",
		"--raw",
		"--minify",
		"--flatten",
		"-o",
		"jsonpath='{.clusters[].cluster.server}'",
	}

	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	clusterServerRaw, err := cmd.Output()
	if err != nil {
		output.Formatter.Error(err, fmt.Sprintf("unable to get server url from kubeconfig; context: %s; stdErr: %s; stdOut: %s", generateSecretFlags.contextName, stderr.String(), cmd.Stdout))
		return "", fmt.Errorf("unable to get server url for %v from kubeconfig", generateSecretFlags.contextName)
	}
	clusterServer := string(clusterServerRaw)
	clusterServer = strings.Trim(clusterServer, "'")
	return clusterServer, nil
}

// generateServiceAccountToken returns the value of a token, or an error.
// Token is created on the cluster for the service account.
func generateServiceAccountToken() (string, error) {
	output.Formatter.Debug("Get the service account token from the remote cluster")
	args := []string{
		fmt.Sprintf("--context=%v", generateSecretFlags.contextName),
		"--namespace",
		generateSecretFlags.namespace,
		"create",
		"token",
		generateSecretFlags.serviceAccount,
		"--duration=8760h",
	}
	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	serviceAccountToken, err := cmd.Output()
	if err != nil {
		output.Formatter.Error(err, fmt.Sprintf("unable to generate service account token from kubeconfig; context: %s; stdErr: %s; stdOut: %s", generateSecretFlags.contextName, stderr.String(), cmd.Stdout))
		return "", fmt.Errorf("unable to generate service account token for %v from kubeconfig", generateSecretFlags.contextName)

	}

	return string(serviceAccountToken), nil
}

func defineKubeConfig(clusterServer, clusterCA, serviceAccountToken string) Config {
	output.Formatter.Debug("Generate a new kubeconfig using the service account token")
	return Config{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []NamedCluster{
			{
				Name: generateSecretFlags.name,
				Cluster: Cluster{
					Server:                   clusterServer,
					CertificateAuthorityData: clusterCA,
				},
			},
		},
		Contexts: []NamedContext{
			{
				Name: generateSecretFlags.name,
				Context: Context{
					Cluster: generateSecretFlags.name,
					User:    generateSecretFlags.serviceAccount,
				},
			},
		},
		CurrentContext: generateSecretFlags.name,
		Users: []NamedUser{
			{
				Name: generateSecretFlags.serviceAccount,
				User: User{
					Token: serviceAccountToken,
				},
			},
		},
	}
}

func checkKubectl() error {
	output.Formatter.Debug("Check that kubectl is in the users path")
	if _, err := exec.LookPath(kubectl); err != nil {
		output.Formatter.Error(err, "kubectl not found in user path")
		return errors.New("kubectl not found in user $PATH")
	}
	return nil
}

func saveKubeConfig(kubeConfig Config, dirPath string) (*os.File, error) {
	output.Formatter.Debug("Save kubeconfig temporarily for testing")
	f, err := os.CreateTemp(dirPath, "kubeconfig")
	if err != nil {
		output.Formatter.Error(err, "Unable to create temporary file for sercet")
		return nil, errors.New("temporary file creation failure.")
	}

	data, err := yaml.Marshal(kubeConfig)
	if err != nil {
		_err := errors.New("Internal error marshaling yaml")
		err_ := tidyFiles(f.Name())
		if err_ != nil {
			output.Formatter.Error(err, "failed to remove temporary kubeconfig")
			_err = fmt.Errorf(err_.Error(), err)
		}
		output.Formatter.Error(err, "marshaling yaml of kubeConfig file")
		return nil, _err
	}

	_, err = f.Write(data)
	if err != nil {
		_err := errors.New("Internal error writing file to disc")
		err_ := tidyFiles(f.Name())
		if err_ != nil {
			output.Formatter.Error(err, "failed to remove temporary kubeconfig")
			_err = fmt.Errorf(err_.Error(), err)
		}
		output.Formatter.Error(err, fmt.Sprintf("error writing of kubeConfig file: %s", f.Name()))
		return nil, _err
	}

	return f, nil

}

func verifyKubeConfigConnection(kubeConfigPath string) error {
	output.Formatter.Debug("Verify the kubeconfig works")
	args := []string{
		fmt.Sprintf("--kubeconfig=%v", kubeConfigPath),
		"version",
	}
	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	_, err := cmd.Output()
	if err != nil {
		output.Formatter.Info("unable to connect to cluster")
		output.Formatter.Debug(fmt.Sprintf("command: %s; stdErr: %s; stdOut: %s", strings.Join(cmd.Args, " "), stderr.String(), cmd.Stdout))
		return errors.New("Failed to verify kubeconfig - unable to connect to cluster")
	}

	return nil
}

func generateSecret(kubeConfig Config) (*Secret, error) {
	output.Formatter.Debug("Generate secret with kubeConfig as data")
	data, err := yaml.Marshal(kubeConfig)
	if err != nil {
		output.Formatter.Error(err, "marshaling yaml of kubeConfig file")
		return nil, errors.New("Internal error marshaling yaml")
	}

	return &Secret{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata: Metadata{
			Name:      generateSecretFlags.name,
			Namespace: generateSecretFlags.namespace,
			Labels: map[string]string{
				"kuadrant.io/multicluster-kubeconfig": "true",
			},
		},
		StringData: map[string]string{
			"kubeconfig": string(data),
		},
	}, nil
}

func saveSecret(secret Secret, dirPath string) (*os.File, error) {
	f, err := os.CreateTemp(dirPath, "secret")
	if err != nil {
		output.Formatter.Error(err, "Unable to create temporary file for sercet")
		return nil, errors.New("temporary file creation failure.")
	}

	data, err := yaml.Marshal(secret)
	if err != nil {
		_err := errors.New("Internal error marshaling yaml")
		err_ := tidyFiles(f.Name())
		if err_ != nil {
			output.Formatter.Error(err, "failed to remove temporary secret")
			_err = fmt.Errorf(err_.Error(), err)
		}
		output.Formatter.Error(err, fmt.Sprintf("error writing secret file: %s", f.Name()))
		return nil, _err
	}

	_, err = f.Write(data)
	if err != nil {
		_err := errors.New("Internal error writing file to disc")
		err_ := tidyFiles(f.Name())
		if err_ != nil {
			output.Formatter.Error(err, "failed to remove temporary secret")
			_err = fmt.Errorf(err_.Error(), err)
		}
		output.Formatter.Error(err, fmt.Sprintf("error writing secret file: %s", f.Name()))
		return nil, _err
	}

	return f, nil
}

func applySecretToCluser(secretFile string) error {
	output.Formatter.Debug("Write secert to main cluster")
	args := []string{
		"apply",
		"--filename",
		secretFile,
	}
	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	_, err := cmd.Output()

	if err != nil {
		output.Formatter.Debug(fmt.Sprintf("command: %s; stdErr: %s; stdOut: %s", strings.Join(cmd.Args, " "), stderr.String(), cmd.Stdout))
		output.Formatter.Error(err, "unable to write secret to cluster")
		return errors.New("unable to write secret to cluster")
	}

	output.Formatter.Info(fmt.Sprintf("Secret %s created in namespace %s", generateSecretFlags.name, generateSecretFlags.namespace))

	return nil
}

func tidyFiles(path string) error {
	output.Formatter.Debug(fmt.Sprintf("Removing file/directory: %s", path))
	if generateSecretFlags.dirty {
		output.Formatter.Debug("Running in a dirty mode not removing the file/directory.")
		return nil
	}
	return os.Remove(path)
}
