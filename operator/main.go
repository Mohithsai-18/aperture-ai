// Package main is the entry point for the Aperture GPU Workload Operator.
// It initializes the controller-runtime manager, registers the GPUWorkload
// CRD scheme, sets up the reconciler and admission webhook, and starts
// the control loop.
package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	aperturev1alpha1 "github.com/aperture-ai/operator/api/v1alpha1"
	"github.com/aperture-ai/operator/controllers"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(aperturev1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager to ensure only one active controller.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// ---------------------------------------------------------------
	// Create the controller manager
	// ---------------------------------------------------------------
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "aperture-gpu-operator.aperture.ai",
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
	})
	if err != nil {
		setupLog.Error(err, "Unable to create manager")
		os.Exit(1)
	}

	// ---------------------------------------------------------------
	// Register the GPUWorkload reconciler
	// ---------------------------------------------------------------
	if err := (&controllers.GPUWorkloadReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "GPUWorkload")
		os.Exit(1)
	}

	// ---------------------------------------------------------------
	// Register the GPUWorkload validating webhook
	// ---------------------------------------------------------------
	// if err := (&aperturev1alpha1.GPUWorkload{}).SetupWebhookWithManager(mgr); err != nil {
	// 	setupLog.Error(err, "Unable to create webhook", "webhook", "GPUWorkload")
	// 	os.Exit(1)
	// }

	// ---------------------------------------------------------------
	// Health and readiness probes
	// ---------------------------------------------------------------
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up readiness check")
		os.Exit(1)
	}

	// ---------------------------------------------------------------
	// Start the manager (blocks until context is cancelled)
	// ---------------------------------------------------------------
	setupLog.Info("Starting Aperture GPU Workload Operator")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
