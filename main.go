package main

import (
	"context"
	"fmt"
	"github.com/go-yaml/yaml"
	"github.com/mgutz/ansi"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/config"
	"k8s.io/component-base/logs"
	"k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/cmd/plugin"
	"math/rand"

	// Import to initialize client auth plugins.
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io/ioutil"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	"os"
	"path/filepath"
	"time"
)

var configFilePath string
var serviceName string

type ZitiFlags struct {
	zConfig string
	service string
}

type Context struct {
	ZConfig string `yaml:"zConfig"`
	Service string `yaml:"service"`
}

type MinKubeConfig struct {
	Contexts []struct {
		Context Context `yaml:"context"`
		Name    string  `yaml:"name"`
	} `yaml:"contexts"`
}

var zFlags = ZitiFlags{}

func main() {
	logrus.SetFormatter(&logrusFormatter{})
	logrus.SetLevel(logrus.WarnLevel)

	rand.Seed(time.Now().UnixNano())
	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()

	// set the wrapper function. This allows modification to the reset Config
	kubeConfigFlags.WrapConfigFn = wrapConfigFn
	var kubeztlOptions cmd.KubectlOptions
	kubeztlOptions.PluginHandler = cmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes)
	kubeztlOptions.Arguments = os.Args
	kubeztlOptions.ConfigFlags = kubeConfigFlags
	kubeztlOptions.IOStreams = genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}

	// create the cobra command and set ConfigFlags
	command := cmd.NewDefaultKubectlCommandWithArgs(kubeztlOptions)

	//set and parse the ziti flags
	command = setZitiFlags(command)
	command.PersistentFlags().Parse(os.Args)

	// try to get the ziti options from the flags
	configFilePath = command.Flag("zConfig").Value.String()
	serviceName = command.Flag("service").Value.String()

	// get the loaded kubeconfig
	kubeconfig := getKubeconfig()

	// if both the config file and service name are not set, parse the kubeconfig file
	if configFilePath == "" || serviceName == "" {
		parseKubeConfig(command, kubeconfig)
	}

	// TODO: once we switch everything over to Cobra commands, we can go back to calling
	// cliflag.InitFlags() (by removing its pflag.Parse() call). For now, we have to set the
	// normalize func and add the go flag set by hand.

	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}

// function for handling the dialing with ziti
func dialFunc(ctx context.Context, network, address string) (net.Conn, error) {
	// Get Ziti Service Name
	service := serviceName
	if service == "" {
		logrus.Error("Service Name not provided")
		os.Exit(1)
	}
	// Load ziti identity configuration
	configFile, err := config.NewFromFile(configFilePath)
	if err != nil {
		logrus.WithError(err).Error("Loading Ziti Identity Config File")
		os.Exit(1)
	}

	context := ziti.NewContextWithConfig(configFile)
	return context.Dial(service)
}

func wrapConfigFn(restConfig *rest.Config) *rest.Config {

	restConfig.Dial = dialFunc
	return restConfig
}

func setZitiFlags(command *cobra.Command) *cobra.Command {

	command.PersistentFlags().StringVarP(&zFlags.zConfig, "zConfig", "", "", "Path to ziti config file")
	command.PersistentFlags().StringVarP(&zFlags.service, "service", "", "", "Service name")

	return command
}

// function for getting the current kubeconfig
func getKubeconfig() clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules,
		configOverrides)

	return kubeConfig
}

func parseKubeConfig(command *cobra.Command, kubeconfig clientcmd.ClientConfig) {
	// attempt to get the kubeconfig path from the command flags
	kubeconfigPath := command.Flag("kubeconfig").Value.String()

	// if the path is not set, attempt to get it from the kubeconfig precedence
	if kubeconfigPath == "" {
		// obtain the list of kubeconfig files from the current kubeconfig
		kubeconfigPrcedence := kubeconfig.ConfigAccess().GetLoadingPrecedence()

		// get the raw API config
		apiConfig, err := kubeconfig.RawConfig()

		if err != nil {
			panic(err)
		}

		// set the ziti options from one of the config files
		getZitiOptionsFromConfigList(kubeconfigPrcedence, apiConfig.CurrentContext)

	} else {
		// get the ziti options form the specified path
		getZitiOptionsFromConfig(kubeconfigPath)
	}

}

func getZitiOptionsFromConfigList(kubeconfigPrcedence []string, currentContext string) {
	// for the kubeconfig files in the precedence
	for _, path := range kubeconfigPrcedence {

		// read the config file
		config := readKubeConfig(path)

		// loop through the context list
		for _, context := range config.Contexts {

			// if the context name matches the current context
			if currentContext == context.Name {

				// set the config file path if it's not already set
				if configFilePath == "" {
					configFilePath = context.Context.ZConfig
				}

				// set the service name if it's not already set
				if serviceName == "" {
					serviceName = context.Context.Service
				}

				break
			}
		}
	}
}

func readKubeConfig(kubeconfig string) MinKubeConfig {
	// get the file name from the path
	filename, _ := filepath.Abs(kubeconfig)

	// read the yaml file
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		panic(err)
	}

	var minKubeConfig MinKubeConfig

	//parse the yaml file
	err = yaml.Unmarshal(yamlFile, &minKubeConfig)
	if err != nil {
		panic(err)
	}

	return minKubeConfig

}

func getZitiOptionsFromConfig(kubeconfig string) {

	// get the config from the path
	config := clientcmd.GetConfigFromFileOrDie(kubeconfig)

	// get the current context
	currentContext := config.CurrentContext

	// read the yaml file
	minKubeConfig := readKubeConfig(kubeconfig)

	var context Context
	// find the context that matches the current context
	for _, ctx := range minKubeConfig.Contexts {

		if ctx.Name == currentContext {
			context = ctx.Context
		}
	}

	// set the config file if not already set
	if configFilePath == "" {
		configFilePath = context.ZConfig
	}

	// set the service name if not already set
	if serviceName == "" {
		serviceName = context.Service
	}
}

type logrusFormatter struct {
}

func (fa *logrusFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	level := toLevel(entry)
	return []byte(fmt.Sprintf("%s\t%s\n", level, entry.Message)), nil
}

func toLevel(entry *logrus.Entry) string {
	switch entry.Level {
	case logrus.PanicLevel:
		return panicColor
	case logrus.FatalLevel:
		return fatalColor
	case logrus.ErrorLevel:
		return errorColor
	case logrus.WarnLevel:
		return warnColor
	case logrus.InfoLevel:
		return infoColor
	case logrus.DebugLevel:
		return debugColor
	case logrus.TraceLevel:
		return traceColor
	default:
		return infoColor
	}
}

var panicColor = ansi.Red + "PANIC" + ansi.DefaultFG
var fatalColor = ansi.Red + "FATAL" + ansi.DefaultFG
var errorColor = ansi.Red + "ERROR" + ansi.DefaultFG
var warnColor = ansi.Yellow + "WARN " + ansi.DefaultFG
var infoColor = ansi.LightGreen + "INFO " + ansi.DefaultFG
var debugColor = ansi.LightBlue + "DEBUG" + ansi.DefaultFG
var traceColor = ansi.LightBlack + "TRACE" + ansi.DefaultFG
