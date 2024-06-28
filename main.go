/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"flag"
	"net/http"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
	"sigs.k8s.io/jobset/pkg/config"
	"sigs.k8s.io/jobset/pkg/controllers"
	"sigs.k8s.io/jobset/pkg/metrics"
	"sigs.k8s.io/jobset/pkg/util/cert"
	"sigs.k8s.io/jobset/pkg/webhooks"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(jobset.AddToScheme(scheme))
	utilruntime.Must(configapi.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var qps float64
	var burst int
	var featureGates string
	var configFile string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Float64Var(&qps, "kube-api-qps", 500, "Maximum QPS to use while talking with Kubernetes API")
	flag.IntVar(&burst, "kube-api-burst", 500, "Maximum burst for throttle while talking with Kubernetes API")
	flag.StringVar(&featureGates, "feature-gates", "", "A set of key=value pairs that describe feature gates for alpha/experimental features.")
	flag.StringVar(&configFile, "config", "",
		"The controller will load its initial configuration from this file. "+
			"Once configured, flags other than feature-gates will not take effect"+
			"Omit this flag to use the default configuration values. ")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := utilfeature.DefaultMutableFeatureGate.Set(featureGates); err != nil {
		setupLog.Error(err, "Unable to set flag gates for known features")
		os.Exit(1)
	}

	metrics.Register()

	var options manager.Options

	kubeConfig := ctrl.GetConfigOrDie()
	if configFile == "" {
		options = ctrl.Options{
			Scheme: scheme,
			Metrics: server.Options{
				BindAddress: metricsAddr,
			},
			WebhookServer: webhook.NewServer(
				webhook.Options{
					Port: 9443,
				}),
			HealthProbeBindAddress: probeAddr,
			LeaderElection:         enableLeaderElection,
			LeaderElectionID:       "6d4f6a47.x-k8s.io",
			// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
			// when the Manager ends. This requires the binary to immediately end when the
			// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
			// speeds up voluntary leader transitions as the new leader don't have to wait
			// LeaseDuration time first.
			//
			// In the default scaffold provided, the program ends immediately after
			// the manager stops, so would be fine to enable this option. However,
			// if you are doing or is intended to do any operation such as perform cleanups
			// after the manager stops then its usage might be unsafe.
			// LeaderElectionReleaseOnCancel: true,
		}
		kubeConfig.QPS = float32(qps)
		kubeConfig.Burst = burst
	} else {
		opts, cfg, err := apply(configFile)
		if err != nil {
			setupLog.Error(err, "Unable to load the configuration")
			os.Exit(1)
		}
		options = opts
		kubeConfig.QPS = *cfg.ClientConnection.QPS
		kubeConfig.Burst = int(*cfg.ClientConnection.Burst)
	}

	mgr, err := ctrl.NewManager(kubeConfig, options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	certsReady := make(chan struct{})
	if err = cert.CertsManager(mgr, certsReady); err != nil {
		setupLog.Error(err, "unable to setup cert rotation")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	if err := controllers.SetupJobSetIndexes(ctx, mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "unable to setup jobset reconciler indexes")
		os.Exit(1)
	}
	if err := controllers.SetupPodIndexes(ctx, mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "unable to setup pod reconciler indexes")
		os.Exit(1)
	}

	// Cert won't be ready until manager starts, so start a goroutine here which
	// will block until the cert is ready before setting up the controllers.
	// Controllers who register after manager starts will start directly.
	go setupControllers(mgr, certsReady)

	setupHealthzAndReadyzCheck(mgr, certsReady)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupControllers(mgr ctrl.Manager, certsReady chan struct{}) {
	// The controllers won't work until the webhooks are operating,
	// and the webhook won't work until the certs are all in places.
	setupLog.Info("waiting for the cert generation to complete")
	<-certsReady
	setupLog.Info("certs ready")

	// Set up JobSet controller.
	jobSetController := controllers.NewJobSetReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("jobset"))
	if err := jobSetController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "JobSet")
		os.Exit(1)
	}

	// Set up pod reconciler.
	podController := controllers.NewPodReconciler(mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("pod"))
	if err := podController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}

	// Set up JobSet validating/defaulting webhook.
	jobSetWebHook, err := webhooks.NewJobSetWebhook(mgr.GetClient())
	if err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "JobSet")
		os.Exit(1)
	}
	if err := jobSetWebHook.SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up webhook", "webhook", "JobSet")
		os.Exit(1)
	}

	// Set up pod mutating and admission webhook.
	podWebhook := webhooks.NewPodWebhook(mgr.GetClient())
	if err := podWebhook.SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up webhook", "webhook", "Pod")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder
}

func setupHealthzAndReadyzCheck(mgr ctrl.Manager, certsReady <-chan struct{}) {
	defer setupLog.Info("both healthz and readyz check are finished and configured")

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Wait for the webhook server to be listening before advertising the
	// Jobset deployment replica as ready. This allows users to wait with sending
	// the first requests, requiring webhooks, until the Jobset deployment is
	// available, so that the early requests are not rejected during the Jobset's
	// startup. We wrap the call to GetWebhookServer in a closure to delay calling
	// the function, otherwise a not fully-initialized webhook server (without
	// ready certs) fails the start of the manager.
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		select {
		case <-certsReady:
			return mgr.GetWebhookServer().StartedChecker()(req)
		default:
			return errors.New("certificates are not ready")
		}
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}

func apply(configFile string) (ctrl.Options, configapi.Configuration, error) {
	options, cfg, err := config.Load(scheme, configFile)
	if err != nil {
		return options, cfg, err
	}
	cfgStr, err := config.Encode(scheme, &cfg)
	if err != nil {
		return options, cfg, err
	}
	setupLog.Info("Successfully loaded configuration", "config", cfgStr)
	return options, cfg, nil
}
