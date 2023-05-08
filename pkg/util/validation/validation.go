/*
Copyright 2023 The Kubernetes Authors.
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

package validation

import (
	"errors"
	v1 "k8s.io/api/batch/v1"
)

const (
	IsNegativeErrorMsg     string = `must be greater than or equal to 0`
	FieldValueNotSupported string = `not supported in the field value`
)

func ValidationJobTemplateSpec(template v1.JobTemplateSpec) []error {
	var allErrs []error

	if template.Spec.Parallelism != nil {
		allErrs = append(allErrs, errors.New("parallelism"+IsNegativeErrorMsg))
	}
	if template.Spec.Completions != nil {
		allErrs = append(allErrs, errors.New("completions"+IsNegativeErrorMsg))
	}
	if template.Spec.ActiveDeadlineSeconds != nil {
		allErrs = append(allErrs, errors.New("activeDeadlineSeconds"+IsNegativeErrorMsg))
	}
	if template.Spec.BackoffLimit != nil {
		allErrs = append(allErrs, errors.New("backoffLimit"+IsNegativeErrorMsg))
	}
	if template.Spec.TTLSecondsAfterFinished != nil {
		allErrs = append(allErrs, errors.New("ttlSecondsAfterFinished"+IsNegativeErrorMsg))
	}
	if template.Spec.CompletionMode != nil {
		if *template.Spec.CompletionMode != v1.NonIndexedCompletion && *template.Spec.CompletionMode != v1.IndexedCompletion {
			allErrs = append(allErrs, errors.New("completionMode"+FieldValueNotSupported))
		}
		if *template.Spec.CompletionMode == v1.IndexedCompletion {
			if template.Spec.Completions == nil {
				allErrs = append(allErrs, errors.New("completions need to be set when completion mode is Indexed"))
			}
		}
	}
	return allErrs
}
