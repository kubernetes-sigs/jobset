package config

import (
	"bytes"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

func fromFile(path string, scheme *runtime.Scheme, cfg *configapi.Configuration) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	codecs := serializer.NewCodecFactory(scheme, serializer.EnableStrict)

	// Regardless of if the bytes are of any external version,
	// it will be read successfully and converted into the internal version
	return runtime.DecodeInto(codecs.UniversalDecoder(), content, cfg)
}

// addTo applies the configuration from cfg to the controller-runtime Options o.
// It only sets values in o if they are not already set and are present in cfg.
func addTo(o *ctrl.Options, cfg *configapi.Configuration) {
	addLeaderElectionTo(o, cfg)

	if o.Metrics.BindAddress == "" && cfg.Metrics.BindAddress != "" {
		o.Metrics.BindAddress = cfg.Metrics.BindAddress
	}

	internalCertManagementEnabled := ptr.Deref(cfg.InternalCertManagement.Enable, false)
	if o.Metrics.CertDir == "" && (cfg.Metrics.CertDir != "" || !internalCertManagementEnabled) {
		o.Metrics.CertDir = cfg.Metrics.CertDir
		if o.Metrics.CertDir == "" {
			o.Metrics.CertDir = "/tmp/k8s-metrics-server/serving-certs"
		}
		o.Metrics.CertName = "tls.crt"
		o.Metrics.KeyName = "tls.key"
	}

	if o.HealthProbeBindAddress == "" && cfg.Health.HealthProbeBindAddress != "" {
		o.HealthProbeBindAddress = cfg.Health.HealthProbeBindAddress
	}

	if o.ReadinessEndpointName == "" && cfg.Health.ReadinessEndpointName != "" {
		o.ReadinessEndpointName = cfg.Health.ReadinessEndpointName
	}

	if o.LivenessEndpointName == "" && cfg.Health.LivenessEndpointName != "" {
		o.LivenessEndpointName = cfg.Health.LivenessEndpointName
	}

	if o.WebhookServer == nil && cfg.Webhook.Port != nil {
		wo := webhook.Options{}
		if cfg.Webhook.Port != nil {
			wo.Port = *cfg.Webhook.Port
		}
		if cfg.Webhook.Host != "" {
			wo.Host = cfg.Webhook.Host
		}
		if cfg.Webhook.CertDir != "" {
			wo.CertDir = cfg.Webhook.CertDir
		}
		o.WebhookServer = webhook.NewServer(wo)
	}
}

func addLeaderElectionTo(o *ctrl.Options, cfg *configapi.Configuration) {
	if cfg.LeaderElection == nil {
		// The source does not have any configuration; noop
		return
	}

	if !o.LeaderElection && cfg.LeaderElection.LeaderElect != nil {
		o.LeaderElection = *cfg.LeaderElection.LeaderElect
	}

	if o.LeaderElectionResourceLock == "" && cfg.LeaderElection.ResourceLock != "" {
		o.LeaderElectionResourceLock = cfg.LeaderElection.ResourceLock
	}

	if o.LeaderElectionNamespace == "" && cfg.LeaderElection.ResourceNamespace != "" {
		o.LeaderElectionNamespace = cfg.LeaderElection.ResourceNamespace
	}

	if o.LeaderElectionID == "" && cfg.LeaderElection.ResourceName != "" {
		o.LeaderElectionID = cfg.LeaderElection.ResourceName
	}

	if o.LeaseDuration == nil && !equality.Semantic.DeepEqual(cfg.LeaderElection.LeaseDuration, metav1.Duration{}) {
		o.LeaseDuration = &cfg.LeaderElection.LeaseDuration.Duration
	}

	if o.RenewDeadline == nil && !equality.Semantic.DeepEqual(cfg.LeaderElection.RenewDeadline, metav1.Duration{}) {
		o.RenewDeadline = &cfg.LeaderElection.RenewDeadline.Duration
	}

	if o.RetryPeriod == nil && !equality.Semantic.DeepEqual(cfg.LeaderElection.RetryPeriod, metav1.Duration{}) {
		o.RetryPeriod = &cfg.LeaderElection.RetryPeriod.Duration
	}
}

func Encode(scheme *runtime.Scheme, cfg *configapi.Configuration) (string, error) {
	codecs := serializer.NewCodecFactory(scheme)
	const mediaType = runtime.ContentTypeYAML
	info, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
	if !ok {
		return "", fmt.Errorf("unable to locate encoder -- %q is not a supported media type", mediaType)
	}

	encoder := codecs.EncoderForVersion(info.Serializer, configapi.GroupVersion)
	buf := new(bytes.Buffer)
	if err := encoder.Encode(cfg, buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Load returns a set of controller options and configuration from the given file, if the config file path is empty
// it used the default configapi values.
func Load(scheme *runtime.Scheme, configFile string) (ctrl.Options, configapi.Configuration, error) {
	var err error
	options := ctrl.Options{
		Scheme: scheme,
	}

	cfg := configapi.Configuration{}
	if configFile == "" {
		scheme.Default(&cfg)
	} else {
		err := fromFile(configFile, scheme, &cfg)
		if err != nil {
			return options, cfg, err
		}
	}
	if err := validate(&cfg).ToAggregate(); err != nil {
		return options, cfg, err
	}
	addTo(&options, &cfg)
	return options, cfg, err
}
