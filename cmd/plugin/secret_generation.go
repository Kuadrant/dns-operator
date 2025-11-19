package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	log = logf.Log.WithName("add-cluster-secret")

	dirtyStr := os.Getenv("KUBECTL_DNS_DIRTY")
	if strings.Compare(strings.ToLower(dirtyStr), "true") == 0 {
		log.V(1).Info("kubectl_kuadrant-dns running in dirty mode")
		generateSecretFlags.dirty = true
	}

	log.V(1).Info("Set the secret name to context name if not set", "context", generateSecretFlags.contextName, "name", generateSecretFlags.name)
	if len(generateSecretFlags.name) == 0 {
		generateSecretFlags.name = generateSecretFlags.contextName
	}

	err := checkKubectl(log)
	if err != nil {
		return err
	}

	clusterCA, err := getClusterCA(log)
	if err != nil {
		return err
	}

	clusterServer, err := getClusterServer(log)
	if err != nil {
		return err
	}

	serviceAccountToken, err := generateServiceAccountToken(log)
	if err != nil {
		return err
	}

	log.V(1).Info("creation directory to store temporary files")
	tmpDir, err := os.MkdirTemp("", "kudectl-dev")
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(log, tmpDir); err != nil {
			log.Error(err, "deferred call failed")
		}
	}()

	kubeConfig := defineKubeConfig(log, clusterServer, clusterCA, serviceAccountToken)
	kubeConfigFile, err := saveKubeConfig(log, kubeConfig, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(log, kubeConfigFile.Name()); err != nil {
			log.Error(err, "deferred call failed")
		}
	}()

	err = verifyKubeConfigConnection(log, kubeConfigFile.Name())
	if err != nil {
		return err
	}

	secret, err := generateSecret(log, kubeConfig)
	if err != nil {
		return err
	}

	secretFile, err := saveSecret(log, *secret, tmpDir)
	if err != nil {
		return err
	}
	defer func() {
		if err := tidyFiles(log, secretFile.Name()); err != nil {
			log.Error(err, "deferred call failed")
		}
	}()

	err = applySecretToCluser(log, secretFile.Name())
	if err != nil {
		return err
	}

	log.Info("The operator should now be able to discover, and connect to this cluster")

	return nil
}

func getClusterCA(log logr.Logger) (string, error) {
	log.V(1).Info("Get the cluster CA certificate from the remote cluster")
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
		if log.V(1).Enabled() {
			log.V(1).Error(err, "unable to get certificate authority from kubeconfig", "context", generateSecretFlags.contextName, "stdErr", stderr.String(), "stdOut", cmd.Stdout)
		}
		return "", fmt.Errorf("unable to get certificate for %v from kubeconfig", generateSecretFlags.contextName)
	}
	clusterCA := string(clusterCARaw)
	clusterCA = strings.Trim(clusterCA, "'")
	return clusterCA, nil

}

func getClusterServer(log logr.Logger) (string, error) {
	log.V(1).Info("Get the cluster server URL from the remote cluster")
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
		if log.V(1).Enabled() {
			log.V(1).Error(err, "unable to get server url from kubeconfig", "context", generateSecretFlags.contextName, "stdErr", stderr.String(), "stdOut", cmd.Stdout)
		}
		return "", fmt.Errorf("unable to get server url for %v from kubeconfig", generateSecretFlags.contextName)
	}
	clusterServer := string(clusterServerRaw)
	clusterServer = strings.Trim(clusterServer, "'")
	return clusterServer, nil
}

// generateServiceAccountToken returns the value of a token, or an error.
// Token is created on the cluster for the service account.
func generateServiceAccountToken(log logr.Logger) (string, error) {
	log.V(1).Info("Get the service account token from the remote cluster")
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
		if log.V(1).Enabled() {
			log.V(1).Error(err, "unable to generate service account token from kubeconfig", "context", generateSecretFlags.contextName, "stdErr", stderr.String(), "stdOut", cmd.Stdout)
		}
		return "", fmt.Errorf("unable to generate service account token for %v from kubeconfig", generateSecretFlags.contextName)

	}

	return string(serviceAccountToken), nil
}

func defineKubeConfig(log logr.Logger, clusterServer, clusterCA, serviceAccountToken string) Config {
	log.V(1).Info("Generate a new kubeconfig using the service account token")
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

func checkKubectl(log logr.Logger) error {
	log.V(1).Info("Check that kubectl is in the users path")
	if _, err := exec.LookPath(kubectl); err != nil {
		if log.V(1).Enabled() {
			log.V(1).Error(err, "kubectl not found in user path")
		}
		return errors.New("kubectl not found in user $PATH")
	}
	return nil
}

func saveKubeConfig(log logr.Logger, kubeConfig Config, dirPath string) (*os.File, error) {
	log.V(1).Info("Save kubeconfig temporarily for testing")
	f, err := os.CreateTemp(dirPath, "kubeconfig")
	if err != nil {
		log.Error(err, "Unable to create temporary file for sercet")
		return nil, errors.New("temporary file creation failure.")
	}

	data, err := yaml.Marshal(kubeConfig)
	if err != nil {
		_err := errors.New("Internal error marshaling yaml")
		err_ := tidyFiles(log, f.Name())
		if err_ != nil {
			log.Error(err, "failed to remove temporary kubeconfig")
			_err = fmt.Errorf(err_.Error(), err)
		}
		log.Error(err, "marshaling yaml of kubeConfig file")
		return nil, _err
	}

	_, err = f.Write(data)
	if err != nil {
		_err := errors.New("Internal error writing file to disc")
		err_ := tidyFiles(log, f.Name())
		if err_ != nil {
			log.Error(err, "failed to remove temporary kubeconfig")
			_err = fmt.Errorf(err_.Error(), err)
		}
		log.Error(err, "error writing of kubeConfig file", "filename", f.Name())
		return nil, _err
	}

	return f, nil

}

func verifyKubeConfigConnection(log logr.Logger, kubeConfigPath string) error {
	log.V(1).Info("Verify the kubeconfig works")
	args := []string{
		fmt.Sprintf("--kubeconfig=%v", kubeConfigPath),
		"version",
	}
	cmd := exec.Command(kubectl, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	_, err := cmd.Output()
	if err != nil {
		log.V(0).Info("unable to connect to cluster")
		log.V(1).Info("command", "cmd", strings.Join(cmd.Args, " "), "stderr", stderr.String(), "stdout", cmd.Stdout)

		return errors.New("Failed to verify kubeconfig - unable to connect to cluster")
	}

	return nil
}

func generateSecret(log logr.Logger, kubeConfig Config) (*Secret, error) {
	log.V(1).Info("Generate secret with kubeConfig as data")
	data, err := yaml.Marshal(kubeConfig)
	if err != nil {
		log.Error(err, "marshaling yaml of kubeConfig file")
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

func saveSecret(log logr.Logger, secret Secret, dirPath string) (*os.File, error) {
	f, err := os.CreateTemp(dirPath, "secret")
	if err != nil {
		log.Error(err, "Unable to create temporary file for sercet")
		return nil, errors.New("temporary file creation failure.")
	}

	data, err := yaml.Marshal(secret)
	if err != nil {
		_err := errors.New("Internal error marshaling yaml")
		err_ := tidyFiles(log, f.Name())
		if err_ != nil {
			log.Error(err, "failed to remove temporary secret")
			_err = fmt.Errorf(err_.Error(), err)
		}
		log.Error(err, "error writing secret file", "filename", f.Name())
		return nil, _err
	}

	_, err = f.Write(data)
	if err != nil {
		_err := errors.New("Internal error writing file to disc")
		err_ := tidyFiles(log, f.Name())
		if err_ != nil {
			log.Error(err, "failed to remove temporary secret")
			_err = fmt.Errorf(err_.Error(), err)
		}
		log.Error(err, "error writing secret file", "filename", f.Name())
		return nil, _err
	}

	return f, nil
}

func applySecretToCluser(log logr.Logger, secretFile string) error {
	log.V(1).Info("Write secert to main cluster")
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
		log.V(1).Info("command", "cmd", strings.Join(cmd.Args, " "), "stderr", stderr.String(), "stdout", cmd.Stdout)
		if log.V(1).Enabled() {
			log.V(1).Error(err, "unable to write secret to cluster")
		}
		log.Info("unable to write secret to cluster")
		return errors.New("unable to write secret to cluster")
	}

	log.Info(fmt.Sprintf("Secert %s created in namespace %s", generateSecretFlags.name, generateSecretFlags.namespace))

	return nil
}

func tidyFiles(log logr.Logger, path string) error {
	log.V(1).Info("Removing file/directory", "path", path)
	if generateSecretFlags.dirty && log.V(1).Enabled() {
		log.V(1).Info("Running in a dirty mode not removing the file/directory.")
		return nil
	}
	return os.Remove(path)
}
