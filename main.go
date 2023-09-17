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
	"net/http"
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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	resourceclient "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfigv1alpha1 "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	autoscalingv1alpha1 "github.com/traas-stack/kapacity/apis/autoscaling/v1alpha1"
	"github.com/traas-stack/kapacity/controllers/autoscaling"
	internalgrpc "github.com/traas-stack/kapacity/internal/grpc"
	"github.com/traas-stack/kapacity/internal/webhook"
	metricprovider "github.com/traas-stack/kapacity/pkg/metric/provider"
	"github.com/traas-stack/kapacity/pkg/metric/provider/metricsapi"
	"github.com/traas-stack/kapacity/pkg/metric/provider/prometheus"
	metricservice "github.com/traas-stack/kapacity/pkg/metric/service"
	"github.com/traas-stack/kapacity/pkg/portrait/algorithm/externaljob/jobcontroller"
	"github.com/traas-stack/kapacity/pkg/portrait/algorithm/externaljob/resultfetcher"
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
	utilruntime.ReallyCrash = false

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

type promConfig struct {
	Address                              string
	AuthInCluster                        bool
	AuthConfigFile                       string
	MetricsConfigFile                    string
	MetricsRelistInterval, MetricsMaxAge time.Duration
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

		enableAdmissionWebhookServer bool

		grpcServerAddr string

		metricProviderType string
		promConfig         promConfig
	)
	flag.StringVar(&logPath, "log-path", "", "The path to write log files. Omit to disable logging to file.")
	flag.DurationVar(&logFileMaxAge, "log-file-max-age", 7*24*time.Hour, "The max age of the log files.")
	flag.DurationVar(&logFileRotationTime, "log-file-rotation-time", 24*time.Hour, "The rotation time of the log file.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&reconcileConcurrency, "reconcile-concurrency", 1, "The reconciliation concurrency of each controller.")
	flag.BoolVar(&enableAdmissionWebhookServer, "serve-admission-webhooks", true, "Enable admission webhook servers.")
	flag.StringVar(&grpcServerAddr, "grpc-server-bind-address", ":9090",
		"The address the gRPC server binds to. Set 0 to disable the gRPC server.")
	flag.StringVar(&metricProviderType, "metric-provider", "prometheus",
		"The name of metric provider. Valid options are prometheus or metrics-api. Defaults to prometheus.")
	flag.StringVar(&promConfig.Address, "prometheus-address", "", "The address of the Prometheus to connect to.")
	flag.BoolVar(&promConfig.AuthInCluster, "prometheus-auth-incluster", false,
		"Use auth details from the in-cluster kubeconfig when connecting to prometheus.")
	flag.StringVar(&promConfig.AuthConfigFile, "prometheus-auth-config", "",
		"The kubeconfig file used to configure auth when connecting to Prometheus.")
	flag.StringVar(&promConfig.MetricsConfigFile, "prometheus-metrics-config", "",
		"The configuration file containing details of how to transform between Prometheus metrics and metrics API resources.")
	flag.DurationVar(&promConfig.MetricsRelistInterval, "prometheus-metrics-relist-interval", 10*time.Minute,
		"The interval at which to re-list the set of all available metrics from Prometheus.")
	flag.DurationVar(&promConfig.MetricsMaxAge, "prometheus-metrics-max-age", 0,
		"The period for which to query the set of available metrics from Prometheus. If not set, it defaults to prometheus-metrics-relist-interval.")
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
		LeaderElectionID:       "kapacity-manager.kapacitystack.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		LeaderElectionReleaseOnCancel: true,
		Controller: ctrlconfigv1alpha1.ControllerConfigurationSpec{
			GroupKindConcurrency: map[string]int{
				"ReplicaProfile.autoscaling.kapacitystack.io":                     reconcileConcurrency,
				"IntelligentHorizontalPodAutoscaler.autoscaling.kapacitystack.io": reconcileConcurrency,
				"HorizontalPortrait.autoscaling.kapacitystack.io":                 reconcileConcurrency,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to build manager")
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		setupLog.Error(err, "unable to build dynamic client")
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

	var metricProvider metricprovider.Interface
	switch metricProviderType {
	case "prometheus":
		metricProvider, err = newPrometheusProviderFromConfig(mgr.GetClient(), dynamicClient, promConfig)
	case "metrics-api":
		metricProvider, err = newMetricsAPIProviderFromConfig(cfg)
	default:
		err = fmt.Errorf("unknown metric provider type %q", metricProviderType)
	}
	if err != nil {
		setupLog.Error(err, "unable to build metric provider", "metricProviderType", metricProviderType)
		os.Exit(1)
	}

	externalHorizontalPortraitAlgorithmJobResultFetchers := initExternalHorizontalPortraitAlgorithmJobResultFetchers()

	if enableAdmissionWebhookServer {
		if err := webhook.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to set up webhook server")
			os.Exit(1)
		}
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
		Client:                             mgr.GetClient(),
		Scheme:                             mgr.GetScheme(),
		EventRecorder:                      mgr.GetEventRecorderFor(autoscaling.HorizontalPortraitControllerName),
		PortraitGenerators:                 initPortraitGenerators(mgr.GetClient(), metricProvider, scaler),
		ExternalAlgorithmJobControllers:    initExternalHorizontalPortraitAlgorithmJobControllers(),
		ExternalAlgorithmJobResultFetchers: externalHorizontalPortraitAlgorithmJobResultFetchers,
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

	if grpcServerAddr != "" && grpcServerAddr != "0" {
		server := internalgrpc.NewServer(grpcServerAddr)
		metricProviderServer := metricservice.NewProviderServer(metricProvider)
		metricProviderServer.RegisterTo(server.ServiceRegistrar())
		if err := mgr.Add(server); err != nil {
			setupLog.Error(err, "unable to set up gRPC server")
			os.Exit(1)
		}
	}

	if runnable, ok := metricProvider.(manager.Runnable); ok {
		if err := mgr.Add(runnable); err != nil {
			setupLog.Error(err, "failed to add runnable metric provider to manager",
				"metricProviderType", metricProviderType)
			os.Exit(1)
		}
	}

	for sourceType, fetcher := range externalHorizontalPortraitAlgorithmJobResultFetchers {
		if err := mgr.Add(fetcher); err != nil {
			setupLog.Error(err, "failed to add external horizontal portrait algorithm job result fetcher to manager",
				"sourceType", sourceType)
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func newPrometheusProviderFromConfig(kubeClient client.Client, kubeDynamicClient dynamic.Interface, config promConfig) (metricprovider.Interface, error) {
	if config.MetricsMaxAge == 0 {
		config.MetricsMaxAge = config.MetricsRelistInterval
	}

	promClient, err := buildPrometheusClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build prometheus client: %v", err)
	}

	metricsConfig, err := prometheus.MetricsConfigFromFile(config.MetricsConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics config from file %q: %v", config.MetricsConfigFile, err)
	}

	return prometheus.NewMetricProvider(kubeClient, kubeDynamicClient, promClient, metricsConfig, config.MetricsRelistInterval, config.MetricsMaxAge)
}

func buildPrometheusClient(config promConfig) (promapi.Client, error) {
	if config.AuthInCluster && config.AuthConfigFile != "" {
		return nil, fmt.Errorf("may not use both in-cluster auth and an explicit kubeconfig at the same time")
	}
	var (
		rt http.RoundTripper = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}
		authConfig *rest.Config
		err        error
	)
	if config.AuthInCluster {
		authConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build in-cluster auth config: %v", err)
		}
	} else if config.AuthConfigFile != "" {
		authConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.AuthConfigFile},
			&clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build auth config from kubeconfig %q: %v", config.AuthConfigFile, err)
		}
	}
	if authConfig != nil {
		rt, err = rest.TransportFor(authConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build transport from auth config: %v", err)
		}
	}
	return promapi.NewClient(promapi.Config{
		Address:      config.Address,
		RoundTripper: rt,
	})
}

func newMetricsAPIProviderFromConfig(kubeConfig *rest.Config) (metricprovider.Interface, error) {
	resourceMetricsClient, err := resourceclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource metrics client: %v", err)
	}
	return metricsapi.NewMetricProvider(resourceMetricsClient), nil
}

func initHorizontalPortraitProviders(client client.Client, eventTrigger chan event.GenericEvent) map[autoscalingv1alpha1.HorizontalPortraitProviderType]portraitprovider.Horizontal {
	providers := make(map[autoscalingv1alpha1.HorizontalPortraitProviderType]portraitprovider.Horizontal)
	providers[autoscalingv1alpha1.StaticHorizontalPortraitProviderType] = portraitprovider.NewStaticHorizontal()
	providers[autoscalingv1alpha1.CronHorizontalPortraitProviderType] = portraitprovider.NewCronHorizontal(eventTrigger)
	providers[autoscalingv1alpha1.DynamicHorizontalPortraitProviderType] = portraitprovider.NewDynamicHorizontal(client, eventTrigger)
	return providers
}

func initPortraitGenerators(client client.Client, metricProvider metricprovider.Interface, scaler *scale.Scaler) map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface {
	generators := make(map[autoscalingv1alpha1.PortraitType]portraitgenerator.Interface)
	generators[autoscalingv1alpha1.ReactivePortraitType] = reactive.NewPortraitGenerator(metricProvider, util.NewCtrlPodLister(client), scaler)
	return generators
}

func initExternalHorizontalPortraitAlgorithmJobControllers() map[autoscalingv1alpha1.PortraitAlgorithmJobType]jobcontroller.Horizontal {
	controllers := make(map[autoscalingv1alpha1.PortraitAlgorithmJobType]jobcontroller.Horizontal)
	// TODO
	return controllers
}

func initExternalHorizontalPortraitAlgorithmJobResultFetchers() map[autoscalingv1alpha1.PortraitAlgorithmResultSourceType]resultfetcher.Horizontal {
	fetchers := make(map[autoscalingv1alpha1.PortraitAlgorithmResultSourceType]resultfetcher.Horizontal)
	// TODO
	return fetchers
}
