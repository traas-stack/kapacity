/*
 Copyright 2023 The Kapacity Authors.

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
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	promapi "github.com/prometheus/client_golang/api"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	scaleclient "k8s.io/client-go/scale"
	"k8s.io/klog/v2"
	resourceclient "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfigv1alpha1 "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/controllers/autoscaling"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
	"github.com/traas-stack/kapacity/pkg/metric/provider/metricsapi"
	"github.com/traas-stack/kapacity/pkg/metric/provider/prometheus"
	portraitgenerator "github.com/traas-stack/kapacity/pkg/portrait/generator"
	"github.com/traas-stack/kapacity/pkg/portrait/generator/reactive"
	portraitprovider "github.com/traas-stack/kapacity/pkg/portrait/provider"
	"github.com/traas-stack/kapacity/pkg/scale"
	"github.com/traas-stack/kapacity/pkg/util"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		logPath             string
		logFileMaxAge       time.Duration
		logFileRotationTime time.Duration

		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		reconcileConcurrency int

		metricProviderType string
		promAddress        string
	)
	flag.StringVar(&logPath, "log-path", "", "The path to write log files. Omit to disable logging to file.")
	flag.DurationVar(&logFileMaxAge, "log-file-max-age", 7*24*time.Hour, "The max age of the log files.")
	flag.DurationVar(&logFileRotationTime, "log-file-rotation-time", 24*time.Hour, "The rotation time of the log file.")
	// TODO(zqzten): use component config to replace below flags
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&reconcileConcurrency, "reconcile-concurrency", 1, "The reconciliation concurrency of each controller.")
	flag.StringVar(&metricProviderType, "metric-provider", "prometheus", "todo")
	flag.StringVar(&promAddress, "prometheus-address", "", "todo")
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if logPath != "" {
		logFileName := logPath + "/kapacity-manager.log"
		rotateLogWriter, err := rotatelogs.New(
			logFileName+".%Y-%m-%d-%H",
			rotatelogs.WithLinkName(logFileName),
			rotatelogs.WithMaxAge(logFileMaxAge),
			rotatelogs.WithRotationTime(logFileRotationTime),
		)
		if err != nil {
			setupLog.Error(err, "failed to new rotate log writer")
			os.Exit(1)
		}
		opts.DestWriter = io.MultiWriter(os.Stderr, rotateLogWriter)
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	klog.SetLogger(logger)

	ctx := ctrl.SetupSignalHandler()

	cfg := ctrl.GetConfigOrDie()
	rest.AddUserAgent(cfg, util.UserAgent)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "manager.kapacity.traas.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		LeaderElectionReleaseOnCancel: true,
		Controller: ctrlconfigv1alpha1.ControllerConfigurationSpec{
			GroupKindConcurrency: map[string]int{
				"ReplicaProfile.autoscaling.kapacity.traas.io":                     reconcileConcurrency,
				"IntelligentHorizontalPodAutoscaler.autoscaling.kapacity.traas.io": reconcileConcurrency,
				"HorizontalPortrait.autoscaling.kapacity.traas.io":                 reconcileConcurrency,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "unable to build discovery client")
		os.Exit(1)
	}
	scalesGetter, err := scaleclient.NewForConfig(cfg, mgr.GetRESTMapper(), dynamic.LegacyAPIPathResolverFunc, scaleclient.NewDiscoveryScaleKindResolver(discoveryClient))
	if err != nil {
		setupLog.Error(err, "unable to build scale client")
		os.Exit(1)
	}
	scaler := scale.NewScaler(scalesGetter, mgr.GetRESTMapper())

	ihpaReconcilerEventTrigger := make(chan event.GenericEvent, 1024)

	metricProvider, err := newMetricProviderFromConfig(metricProviderType, cfg, mgr.GetClient(), promAddress)
	if err != nil {
		setupLog.Error(err, "unable to build metric provider")
		os.Exit(1)
	}

	if err := (&autoscaling.ReplicaProfileReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		EventRecorder: mgr.GetEventRecorderFor(autoscaling.ReplicaProfileControllerName),
		Scaler:        scaler,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ReplicaProfile")
		os.Exit(1)
	}
	if err := (&autoscaling.IntelligentHorizontalPodAutoscalerReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		EventRecorder:     mgr.GetEventRecorderFor(autoscaling.IHPAControllerName),
		PortraitProviders: initHorizontalPortraitProviders(mgr.GetClient(), ihpaReconcilerEventTrigger),
		EventTrigger:      ihpaReconcilerEventTrigger,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IntelligentHorizontalPodAutoscaler")
		os.Exit(1)
	}
	if err := (&autoscaling.HorizontalPortraitReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		EventRecorder:      mgr.GetEventRecorderFor(autoscaling.HorizontalPortraitControllerName),
		PortraitGenerators: initHorizontalPortraitGenerators(mgr.GetClient(), metricProvider, scaler),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "HorizontalPortrait")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

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

func newMetricProviderFromConfig(metricProviderType string, kubeConfig *rest.Config, kubeClient client.Client, promAddress string) (metricprovider.Interface, error) {
	switch metricProviderType {
	case "prometheus":
		// TODO(zqzten): support more configs
		promConfig := promapi.Config{
			Address: promAddress,
		}
		promClient, err := promapi.NewClient(promConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build prometheus client: %v", err)
		}
		return prometheus.NewMetricProvider(kubeClient, promClient, 3*time.Minute)
	case "metrics-api":
		resourceMetricsClient, err := resourceclient.NewForConfig(kubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build resource metrics client: %v", err)
		}
		return metricsapi.NewMetricProvider(resourceMetricsClient), nil
	default:
		return nil, fmt.Errorf("unknown metric provider type %q", metricProviderType)
	}
}

func initHorizontalPortraitProviders(client client.Client, eventTrigger chan event.GenericEvent) map[autoscalingv1alpha1.HorizontalPortraitProviderType]portraitprovider.Horizontal {
	providers := make(map[autoscalingv1alpha1.HorizontalPortraitProviderType]portraitprovider.Horizontal)
	providers[autoscalingv1alpha1.StaticHorizontalPortraitProviderType] = portraitprovider.NewStaticHorizontal()
	providers[autoscalingv1alpha1.CronHorizontalPortraitProviderType] = portraitprovider.NewCronHorizontal(eventTrigger)
	providers[autoscalingv1alpha1.DynamicHorizontalPortraitProviderType] = portraitprovider.NewDynamicHorizontal(client, eventTrigger)
	return providers
}

func initHorizontalPortraitGenerators(client client.Client, metricProvider metricprovider.Interface, scaler *scale.Scaler) map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface {
	generators := make(map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface)
	generators[autoscalingv1alpha1.ReactivePortraitType] = reactive.NewPortraitGenerator(client, metricProvider, scaler)
	return generators
}
