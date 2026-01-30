package config

import (
	"strings"

	apimachineryvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
	"sigs.k8s.io/jobset/pkg/util/tlsconfig"
)

var (
	internalCertManagementPath = field.NewPath("internalCertManagement")
	tlsPath                    = field.NewPath("tls")
)

func validate(c *configapi.Configuration) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, validateInternalCertManagement(c)...)
	allErrs = append(allErrs, validateTLS(c)...)
	return allErrs
}

func validateInternalCertManagement(c *configapi.Configuration) field.ErrorList {
	var allErrs field.ErrorList
	if c.InternalCertManagement == nil || !ptr.Deref(c.InternalCertManagement.Enable, false) {
		return allErrs
	}
	if svcName := c.InternalCertManagement.WebhookServiceName; svcName != nil {
		if errs := apimachineryvalidation.IsDNS1035Label(*svcName); len(errs) != 0 {
			allErrs = append(allErrs, field.Invalid(internalCertManagementPath.Child("webhookServiceName"), svcName, strings.Join(errs, ",")))
		}
	}
	if secName := c.InternalCertManagement.WebhookSecretName; secName != nil {
		if errs := apimachineryvalidation.IsDNS1123Subdomain(*secName); len(errs) != 0 {
			allErrs = append(allErrs, field.Invalid(internalCertManagementPath.Child("webhookSecretName"), secName, strings.Join(errs, ",")))
		}
	}
	return allErrs
}

func validateTLS(c *configapi.Configuration) field.ErrorList {
	var allErrs field.ErrorList
	if c.TLS == nil {
		return allErrs
	}

	// Validate using ParseTLSOptions first - this provides clearer error messages
	// for invalid input (including TLS 1.0/1.1 rejection and invalid cipher suites)
	_, err := tlsconfig.ParseTLSOptions(c.TLS)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(tlsPath.Root(), c.TLS, err.Error()))
		return allErrs
	}

	// TLS 1.3 cipher suites are not configurable in Go's crypto/tls package.
	// When TLS 1.3 is set as the minimum version, cipher suites must not be specified.
	if c.TLS.MinVersion == "VersionTLS13" && len(c.TLS.CipherSuites) > 0 {
		allErrs = append(allErrs, field.Invalid(tlsPath.Child("cipherSuites"),
			c.TLS.CipherSuites, "may not be specified when `minVersion` is 'VersionTLS13'"))
	}

	return allErrs
}
