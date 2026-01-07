/*
Copyright 2025 The Kubernetes Authors.
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
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1apply "k8s.io/client-go/applyconfigurations/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	jobset "sigs.k8s.io/jobset/api/jobset/v1alpha2"
)

const (
	NamespaceFile             = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	EnvJobSetName             = "JOBSET_NAME"
	EnvPodName                = "POD_NAME"
	EnvInPlaceRestartExitCode = "IN_PLACE_RESTART_EXIT_CODE"
	EnvWorkerCommand          = "WORKER_COMMAND"
	ControllerName            = "in-place-restart-agent"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(jobset.AddToScheme(scheme))
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{
		Development: true,
	})))
	env := parseEnvOrDie()
	mgr := createManagerOrDie(env)
	setupInPlaceRestartAgentOrDie(mgr, env)
	
	start(mgr)
}

// createManagerOrDie creates a controller manager or exits with an error
func createManagerOrDie(env env) ctrl.Manager {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// Disable metrics
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		// Disable health probe
		HealthProbeBindAddress: "0",
		// Disable leader election
		LeaderElection: false,
		// Only watch the associated JobSet
		// This is done to reduce the network traffic as much as possible
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				env.Namespace: {},
			},
			ByObject: map[client.Object]cache.ByObject{
				&jobset.JobSet{}: {
					Field: fields.OneTermEqualSelector("metadata.name", env.JobSetName),
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	return mgr
}

// setupInPlaceRestartAgentOrDie sets up the in-place restart agent controller with the manager or exits with an error
func setupInPlaceRestartAgentOrDie(mgr ctrl.Manager, env env) {
	inPlaceRestartAgent := NewInPlaceRestartAgent(
		mgr.GetClient(),
		env.Namespace,
		env.PodName,
		env.WorkerCommand,
		env.InPlaceRestartExitCode,
	)
	if err := inPlaceRestartAgent.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", ControllerName)
		os.Exit(1)
	}
}

// start starts the controller manager and waits for it to stop
// It also handles signals to exit with an appropriate exit code like 128 + signal number instead of 0
func start(mgr ctrl.Manager) {
	// Create a context that is cancelled when a signal is received
	// This is done to make the program exit with an appropriate exit code like 128 + signal number instead of 0
	ctx, cancel := context.WithCancel(context.Background())
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	var capturedSignal os.Signal
	go func() {
		capturedSignal = <-signalChannel
		setupLog.Info("received signal, shutting down", "signal", capturedSignal)
		cancel()
	}()

	// Start manager
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	// Exit with 128 + signal number if the manager stopped due to a signal
	if capturedSignal != nil {
		// If received a signal, exit with 128 + signal number
		if signal, ok := capturedSignal.(syscall.Signal); ok {
			os.Exit(128 + int(signal))
		}
		// Fallback if casting fails (unlikely for SIGINT/SIGTERM)
		os.Exit(1)
	}

	// If the manager stopped without an error and without a signal, exit with 0
	os.Exit(0)
}

// env represents the environment variables
type env struct {
	Namespace              string
	JobSetName             string
	PodName                string
	InPlaceRestartExitCode int
	WorkerCommand          string
}

// parseEnvOrDie parses the environment variables and returns an env struct
// It reads the namespace from the mounted service account file to reduce the number of env vars
// It exits with an error if any of the variables are not set
func parseEnvOrDie() env {
	rawNamespace, err := os.ReadFile(NamespaceFile)
	if err != nil {
		setupLog.Error(err, "unable to read namespace file. Please check if pod.spec.automountServiceAccountToken or serviceAccount.automountServiceAccountToken are set to false", "file", NamespaceFile)
		os.Exit(1)
	}
	namespace := string(rawNamespace)
	jobSetName := getEnvOrDie(EnvJobSetName)
	podName := getEnvOrDie(EnvPodName)
	rawInPlaceRestartExitCode := getEnvOrDie(EnvInPlaceRestartExitCode)
	inPlaceRestartExitCode, err := strconv.Atoi(rawInPlaceRestartExitCode)
	if err != nil {
		setupLog.Error(err, "invalid env var value", "name", EnvInPlaceRestartExitCode, "value", rawInPlaceRestartExitCode)
		os.Exit(1)
	}
	workerCommand := getEnvOrDie(EnvWorkerCommand)

	return env{
		Namespace:              namespace,
		JobSetName:             jobSetName,
		PodName:                podName,
		InPlaceRestartExitCode: inPlaceRestartExitCode,
		WorkerCommand:          workerCommand,
	}
}

// getEnvOrDie returns the value of an environment variable or exits with an error if it is not set
func getEnvOrDie(name string) string {
	value := os.Getenv(name)
	if value == "" {
		setupLog.Error(nil, "env var must be set", "name", name, "value", value)
		os.Exit(1)
	}

	return value
}

// InPlaceRestartAgent is the controller that handles the in-place restart logic at the Pod level
type InPlaceRestartAgent struct {
	client.Client
	Namespace                string
	PodName                  string
	InPlaceRestartExitCode   int
	WorkerCommand            string
	PodInPlaceRestartAttempt *int32
	IsBarrierActive          bool
	Exit                     func(int)                           // Required for testing
	StartWorker              func(context.Context, string) error // Required for testing
}

// NewInPlaceRestartAgent creates a new InPlaceRestartAgent
func NewInPlaceRestartAgent(client client.Client, namespace string, podName string, workerCommand string, inPlaceRestartExitCode int) *InPlaceRestartAgent {
	return &InPlaceRestartAgent{
		Client:                   client,
		Namespace:                namespace,
		PodName:                  podName,
		InPlaceRestartExitCode:   inPlaceRestartExitCode,
		WorkerCommand:            workerCommand,
		PodInPlaceRestartAttempt: nil,
		IsBarrierActive:          true,
		Exit:                     os.Exit,
		StartWorker: func(ctx context.Context, command string) error {
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			cmd := exec.CommandContext(ctx, shell, "-c", command)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return cmd.Run()
		},
	}
}

// SetupWithManager sets up the in-place restart agent with the manager
// Only trigger reconcile when the associated JobSet is affected
func (r *InPlaceRestartAgent) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&jobset.JobSet{}).
		Complete(r)
}

// Reconcile handles the in-place restart logic at the Pod level
func (r *InPlaceRestartAgent) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log = log.WithValues("namespace", r.Namespace, "podName", r.PodName, "inPlaceRestartExitCode", r.InPlaceRestartExitCode, "workerCommand", r.WorkerCommand, "podInPlaceRestartAttempt", r.PodInPlaceRestartAttempt, "isBarrierActive", r.IsBarrierActive)
	ctx = ctrl.LoggerInto(ctx, log)
	log.Info("reconciling")

	// Get associated JobSet
	var js jobset.JobSet
	if err := r.Get(ctx, req.NamespacedName, &js); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	currentInPlaceRestartAttempt := js.Status.CurrentInPlaceRestartAttempt
	previousInPlaceRestartAttempt := js.Status.PreviousInPlaceRestartAttempt
	log = log.WithValues("currentInPlaceRestartAttempt", currentInPlaceRestartAttempt, "previousInPlaceRestartAttempt", previousInPlaceRestartAttempt)
	ctx = ctrl.LoggerInto(ctx, log)

	// Handle start up
	// The Pod in-place restart attempt is set only once per agent execution, instead of possibly setting it multiple times in different reconciliations
	// This could technically be done in the main function by getting the associated JobSet and patching the Pod, but it is done in the reconcile function to reuse the manager client and its watch
	// This is done to decrease the impact on the API server as much as possible
	// The startup is indicated by the Pod in-place restart attempt being not being set yet
	if r.PodInPlaceRestartAttempt == nil {
		var newPodInPlaceRestartAttempt int32
		// If the JobSet current in-place restart attempt has not been set yet, it means that the JobSet is starting for the first time
		// So set the Pod in-place restart attempt to 0
		if currentInPlaceRestartAttempt == nil {
			newPodInPlaceRestartAttempt = 0
			// Otherwise, it means that the JobSet is restarting
			// So set the Pod in-place restart attempt to the JobSet current in-place restart attempt + 1
			// This will make sure the agent keeps waiting at the barrier until the JobSet controller lifts it (i.e., update the JobSet current in-place restart attempt)
		} else {
			newPodInPlaceRestartAttempt = *currentInPlaceRestartAttempt + 1
		}
		err := r.patchPodInPlaceRestartAttempt(ctx, newPodInPlaceRestartAttempt)
		return ctrl.Result{}, err
	}

	// Handle in-place restart
	// If the Pod in-place restart attempt is less than or equal to the previous in-place restart attempt, it means that the JobSet controller has marked the Pod in-place restart attempt as outdated
	// Which means that the Pod should be restarted to reach the new in-place restart attempt
	// So exit with the in-place restart exit code
	// This will trigger container restart since Pod.spec.restartPolicy = OnFailure
	// Once RestartAllContainers is released upstream (k8s 1.35), this will trigger in-place container restart since Pod.spec.initContainers[].restartPolicyRules[].action = RestartAllContainers
	if r.PodInPlaceRestartAttempt != nil && previousInPlaceRestartAttempt != nil && *r.PodInPlaceRestartAttempt <= *previousInPlaceRestartAttempt {
		log.Info("exiting agent with in-place restart exit code to restart this Pod in-place", "exitCode", r.InPlaceRestartExitCode)
		r.Exit(r.InPlaceRestartExitCode)
		return ctrl.Result{}, nil // unreachable
	}

	// Handle barrier lift
	// If the barrier is active and the Pod in-place restart attempt is equal to the JobSet current in-place restart attempt, it means that the JobSet controller has marked the Pod in-place restart attempt as synced with the other Pods
	// So execute the worker command
	// TODO(k8s 1.35): Once RestartAllContainers is released upstream, this should succeed a start up probe instead
	if r.IsBarrierActive && r.PodInPlaceRestartAttempt != nil && currentInPlaceRestartAttempt != nil && *r.PodInPlaceRestartAttempt == *currentInPlaceRestartAttempt {
		go r.executeWorkerCommand(ctx)
		r.IsBarrierActive = false
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// patchPodInPlaceRestartAttempt updates the Pod in-place restart attempt annotation with server-side apply
// It also updates the in-memory Pod in-place restart attempt
func (r *InPlaceRestartAgent) patchPodInPlaceRestartAttempt(ctx context.Context, newPodInPlaceRestartAttempt int32) error {
	log := ctrl.LoggerFrom(ctx)

	// Update the Pod in-place restart attempt annotation with server-side apply
	podApplyConfig := corev1apply.Pod(r.PodName, r.Namespace).
		WithAnnotations(map[string]string{
			jobset.InPlaceRestartAttemptKey: strconv.Itoa(int(newPodInPlaceRestartAttempt)),
		})
	if err := r.Apply(ctx, podApplyConfig, client.FieldOwner(ControllerName), client.ForceOwnership); err != nil {
		log.Error(err, "unable to patch Pod annotation", "newPodInPlaceRestartAttempt", newPodInPlaceRestartAttempt)
		return err
	}

	// If apply succeeds, update the in-memory Pod in-place restart attempt
	r.PodInPlaceRestartAttempt = &newPodInPlaceRestartAttempt
	log.Info("successfully updated Pod in-place restart attempt annotation", "newPodInPlaceRestartAttempt", newPodInPlaceRestartAttempt)

	return nil
}

// executeWorkerCommand executes the worker command
// It makes sure the program exits with the worker command exit code
func (r *InPlaceRestartAgent) executeWorkerCommand(ctx context.Context) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("executing worker command")
	if err := r.StartWorker(ctx, r.WorkerCommand); err != nil {
		log.Error(err, "worker command failed")
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Info("exiting agent with same exit code of the worker command", "exitCode", exitErr.ExitCode())
			r.Exit(exitErr.ExitCode())
			return
		}
		log.Info("worker command failed with unknown error. Exiting agent with exit code 1", "exitCode", 1)
		r.Exit(1)
		return
	}

	log.Info("worker command finished successfully. Exiting agent with exit code 0", "exitCode", 0)
	r.Exit(0)
}
