/*
Copyright 2021.

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
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/medik8s/common/pkg/lease"
	"go.uber.org/zap/zapcore"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"

	nodemaintenancev1beta1 "github.com/medik8s/node-maintenance-operator/v5/api/v1beta1"
	"github.com/medik8s/node-maintenance-operator/v5/internal/controller"
	webhookv1beta1 "github.com/medik8s/node-maintenance-operator/v5/internal/webhook/v1beta1"
	"github.com/medik8s/node-maintenance-operator/v5/pkg/utils"
	"github.com/medik8s/node-maintenance-operator/v5/version"
	//+kubebuilder:scaffold:imports
)

const (
	WebhookCertDir  = "/apiserver.local.config/certificates"
	WebhookCertName = "apiserver.crt"
	WebhookKeyName  = "apiserver.key"
	operatorName    = "NodeMaintenance"
)

var (
	scheme   = k8sruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.Install(scheme))

	utilruntime.Must(nodemaintenancev1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=apiservers,verbs=get;list;watch

func main() {
	var (
		metricsAddr, probeAddr            string
		enableLeaderElection, enableHTTP2 bool
		webhookOpts                       webhook.Options
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "If HTTP/2 should be enabled for the metrics and webhook servers.")

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	printVersion()

	configureWebhookOpts(&webhookOpts, enableHTTP2)

	var tlsOpts []func(*tls.Config)
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.NextProtos = []string{"http/1.1"}
		})
	}

	cfg := ctrl.GetConfigOrDie()
	setupClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create setup client")
		os.Exit(1)
	}

	// Named isOpenShiftTLS to avoid shadowing the isOpenShift variable used for webhook setup below
	isOpenShiftTLS := true
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fetchCancel()
	tlsProfile, err := openshifttls.FetchAPIServerTLSProfile(fetchCtx, setupClient)
	if err != nil {
		if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
			setupLog.Info("Not on OpenShift, using default TLS settings")
			isOpenShiftTLS = false
		} else {
			setupLog.Error(err, "failed to fetch TLS profile")
			os.Exit(1)
		}
	}

	if isOpenShiftTLS {
		tlsConfig, unsupported := openshifttls.NewTLSConfigFromProfile(tlsProfile)
		if len(unsupported) > 0 {
			setupLog.Info("Unsupported TLS ciphers ignored", "ciphers", unsupported)
		}
		tlsOpts = append(tlsOpts, tlsConfig)
		webhookOpts.TLSOpts = append(webhookOpts.TLSOpts, tlsConfig)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress:    metricsAddr,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
			TLSOpts:        tlsOpts,
		},
		WebhookServer:          webhook.NewServer(webhookOpts),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "135b1886.medik8s.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	cl := mgr.GetClient()
	leaseManagerInitializer := &leaseManagerInitializer{cl: cl}
	if err := mgr.Add(leaseManagerInitializer); err != nil {
		setupLog.Error(err, "unable to set up lease Manager", "lease", operatorName)
		os.Exit(1)
	}

	openshiftCheck, err := utils.NewOpenshiftValidator(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "failed to check if we run on Openshift")
		os.Exit(1)
	}
	isOpenShift := openshiftCheck.IsOpenshiftSupported()
	if isOpenShift {
		setupLog.Info("NMO was installed on Openshift cluster")
	}

	if err = (&controller.NodeMaintenanceReconciler{
		Client:       cl,
		Scheme:       mgr.GetScheme(),
		MgrConfig:    mgr.GetConfig(),
		LeaseManager: leaseManagerInitializer,
		Recorder:     mgr.GetEventRecorderFor(operatorName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", operatorName)
		os.Exit(1)
	}
	if err = webhookv1beta1.SetupNodeMaintenanceWebhookWithManager(isOpenShift, mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", operatorName)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	if isOpenShiftTLS {
		watcher := &openshifttls.SecurityProfileWatcher{
			Client:                mgr.GetClient(),
			InitialTLSProfileSpec: tlsProfile,
			OnProfileChange: func(_ context.Context, _, _ configv1.TLSProfileSpec) {
				setupLog.Info("TLS profile changed, restarting")
				cancel()
			},
		}
		if err := watcher.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up TLS profile watcher")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func printVersion() {
	setupLog.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	setupLog.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	setupLog.Info(fmt.Sprintf("Git Commit: %s", version.GitCommit))
	setupLog.Info(fmt.Sprintf("Build Date: %s", version.BuildDate))
}

type leaseManagerInitializer struct {
	cl client.Client
	lease.Manager
}

func (ls *leaseManagerInitializer) Start(context.Context) error {
	var err error
	ls.Manager, err = lease.NewManager(ls.cl, controller.LeaseHolderIdentity)
	return err
}

func configureWebhookOpts(webhookOpts *webhook.Options, enableHTTP2 bool) {

	certs := []string{filepath.Join(WebhookCertDir, WebhookCertName), filepath.Join(WebhookCertDir, WebhookKeyName)}
	certsInjected := true
	for _, fname := range certs {
		if _, err := os.Stat(fname); err != nil {
			certsInjected = false
			break
		}
	}
	if certsInjected {
		webhookOpts.CertDir = WebhookCertDir
		webhookOpts.CertName = WebhookCertName
		webhookOpts.KeyName = WebhookKeyName
		webhookOpts.TLSOpts = []func(*tls.Config){}
	} else {
		setupLog.Info("OLM injected certs for webhooks not found")
	}
	// disable http/2 for mitigating relevant CVEs
	if !enableHTTP2 {
		webhookOpts.TLSOpts = append(webhookOpts.TLSOpts,
			func(c *tls.Config) {
				c.NextProtos = []string{"http/1.1"}
			},
		)
		setupLog.Info("HTTP/2 for webhooks disabled")
	} else {
		setupLog.Info("HTTP/2 for webhooks enabled")
	}

}
