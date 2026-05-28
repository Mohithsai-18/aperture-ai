package v1alpha1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var gpuworkloadlog = logf.Log.WithName("gpuworkload-webhook")

// SetupWebhookWithManager registers the validating webhook for GPUWorkload.
func (r *GPUWorkload) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-aperture-ai-v1alpha1-gpuworkload,mutating=false,failurePolicy=fail,sideEffects=None,groups=aperture.ai,resources=gpuworkloads,verbs=create;update,versions=v1alpha1,name=vgpuworkload.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &GPUWorkload{}

// ValidateCreate validates the GPUWorkload on creation.
// Rules enforced:
//   - modelName must not be empty
//   - gpu must be between 1 and 8 (inclusive)
//   - partitionMode must be "MPS" or "MIG"
//   - tokenQuota must be ≤ 100000
//   - kvCacheGB must be ≥ 1
func (r *GPUWorkload) ValidateCreate() (admission.Warnings, error) {
	gpuworkloadlog.Info("Validating GPUWorkload creation", "name", r.Name)

	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// Rule 1: modelName must not be empty.
	if r.Spec.ModelName == "" {
		allErrs = append(allErrs, field.Required(
			specPath.Child("modelName"),
			"modelName is required and must not be empty",
		))
	}

	// Rule 2: gpu must be between 0 and 8.
	if r.Spec.GPU < 0 || r.Spec.GPU > 8 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("gpu"),
			r.Spec.GPU,
			"gpu count must be between 0 and 8 (inclusive)",
		))
	}

	// Rule 3: partitionMode must be "MPS" or "MIG".
	if r.Spec.PartitionMode != "MPS" && r.Spec.PartitionMode != "MIG" {
		allErrs = append(allErrs, field.NotSupported(
			specPath.Child("partitionMode"),
			r.Spec.PartitionMode,
			[]string{"MPS", "MIG"},
		))
	}

	// Rule 4: tokenQuota must not exceed 100000.
	if r.Spec.TokenQuota > 100000 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("tokenQuota"),
			r.Spec.TokenQuota,
			"tokenQuota must be ≤ 100000",
		))
	}

	// Rule 5: tokenQuota must be positive.
	if r.Spec.TokenQuota < 1 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("tokenQuota"),
			r.Spec.TokenQuota,
			"tokenQuota must be ≥ 1",
		))
	}

	// Rule 6: kvCacheGB must be positive.
	if r.Spec.KVCacheGB < 1 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("kvCacheGB"),
			r.Spec.KVCacheGB,
			"kvCacheGB must be ≥ 1",
		))
	}

	if len(allErrs) > 0 {
		gpuworkloadlog.Info("Validation failed", "name", r.Name, "errors", allErrs)
		return nil, fmt.Errorf("validation failed for GPUWorkload %q: %s",
			r.Name, allErrs.ToAggregate().Error())
	}

	gpuworkloadlog.Info("Validation passed", "name", r.Name)
	return nil, nil
}

// ValidateUpdate validates the GPUWorkload on update.
// Applies the same rules as ValidateCreate.
func (r *GPUWorkload) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	gpuworkloadlog.Info("Validating GPUWorkload update", "name", r.Name)
	return r.ValidateCreate()
}

// ValidateDelete validates the GPUWorkload on deletion.
// No validation rules are enforced on deletion.
func (r *GPUWorkload) ValidateDelete() (admission.Warnings, error) {
	gpuworkloadlog.Info("Validating GPUWorkload deletion", "name", r.Name)
	return nil, nil
}
