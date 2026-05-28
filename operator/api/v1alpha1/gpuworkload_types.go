// Package v1alpha1 contains API Schema definitions for the aperture v1alpha1 API group.
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GPUWorkloadSpec defines the desired state of a GPU-accelerated inference workload.
type GPUWorkloadSpec struct {
	// ModelName is the HuggingFace model identifier to serve (e.g., "facebook/opt-125m").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ModelName string `json:"modelName"`

	// GPU is the number of GPUs to allocate for this workload (1-8).
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=8
	// +kubebuilder:default=1
	GPU int `json:"gpu,omitempty"`

	// PartitionMode specifies GPU partitioning strategy: "MPS" or "MIG".
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=MPS;MIG
	// +kubebuilder:default=MPS
	PartitionMode string `json:"partitionMode,omitempty"`

	// TokenQuota is the maximum number of tokens this workload can generate.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	TokenQuota int `json:"tokenQuota,omitempty"`

	// KVCacheGB is the size of the KV cache in gigabytes for paged attention.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	KVCacheGB int `json:"kvCacheGB,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations are attached to the pod to tolerate any node's taints.
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// PriorityClassName indicates the importance of this workload.
	// +kubebuilder:validation:Optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
}

// GPUWorkloadPhase represents the lifecycle phase of a GPUWorkload.
type GPUWorkloadPhase string

const (
	// PhasePending indicates the workload has been accepted but the pod is not yet running.
	PhasePending GPUWorkloadPhase = "Pending"
	// PhaseRunning indicates the inference pod is running and serving requests.
	PhaseRunning GPUWorkloadPhase = "Running"
	// PhaseCompleted indicates the workload has finished successfully.
	PhaseCompleted GPUWorkloadPhase = "Completed"
	// PhaseFailed indicates the workload has encountered an unrecoverable error.
	PhaseFailed GPUWorkloadPhase = "Failed"
)

// GPUWorkloadStatus defines the observed state of a GPUWorkload.
type GPUWorkloadStatus struct {
	// Phase is the current lifecycle phase (Pending, Running, Completed, Failed).
	// +kubebuilder:validation:Optional
	Phase GPUWorkloadPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of an object's state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// PodName is the name of the Kubernetes Pod running the inference server.
	// +kubebuilder:validation:Optional
	PodName string `json:"podName,omitempty"`

	// Message provides human-readable detail about the current status.
	// +kubebuilder:validation:Optional
	Message string `json:"message,omitempty"`

	// StartTime represents the time when the workload was acknowledged by the operator.
	// +kubebuilder:validation:Optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// ObservedGeneration is the most recent generation observed for this GPUWorkload.
	// It corresponds to the workload's generation, which is updated on mutation by the API Server.
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.modelName`
// +kubebuilder:printcolumn:name="GPU",type=integer,JSONPath=`.spec.gpu`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GPUWorkload is the Schema for the gpuworkloads API.
// It represents a GPU-accelerated inference workload managed by the Aperture operator.
type GPUWorkload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GPUWorkloadSpec   `json:"spec,omitempty"`
	Status GPUWorkloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GPUWorkloadList contains a list of GPUWorkload resources.
type GPUWorkloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GPUWorkload `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GPUWorkload{}, &GPUWorkloadList{})
}
