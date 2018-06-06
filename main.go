package main

import (
	"flag"
	"time"

	"github.com/golang/glog"
	// If new flags are needed this will need to be reconfigured
	flags "k8s.io/ingress-gce/pkg/flags"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// this package is only need for authing with CKE clusters.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	flags.Register()
	flag.Parse()

	stopCh := make(chan struct{})

	config, err := determineConfig()
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
		return
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	controller := NewController(kubeClient, kubeInformerFactory)

	go kubeInformerFactory.Start(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		glog.Fatalf("Error running controller: %s", err.Error())
	}
}

// determineConfig determines if we are running in a cluster or out side
// and gets the appropriate configuration to talk with kubernetes
func determineConfig() (*rest.Config, error) {
	var config *rest.Config
	var err error

	// determine whether to use in cluster config or out of cluster config
	// if kuebconfigPath is not specified, default to in cluster config
	// otherwise, use out of cluster config
	if flags.F.KubeConfigFile == "" {
		glog.Info("Using in cluster k8s config")
		config, err = rest.InClusterConfig()
	} else {
		glog.Info("Using out of cluster k8s config:", flags.F.KubeConfigFile)

		config, err = clientcmd.BuildConfigFromFlags("", flags.F.KubeConfigFile)
	}

	if err != nil {
		return nil, err
	}

	return config, nil
}
