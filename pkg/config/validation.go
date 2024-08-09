package config

import (
	"strings"

	apimachineryvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	configapi "sigs.k8s.io/jobset/api/config/v1alpha1"
)

var (
	internalCertManagementPath = field.NewPath("internalCertManagement")
)

func validate(c *configapi.Configuration) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, validateInternalCertManagement(c)...)
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
