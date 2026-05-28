// Package controllers implements reconciliation logic for Aperture CRDs.
package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aperturev1alpha1 "github.com/aperture-ai/operator/api/v1alpha1"
)

const (
	// finalizerName is used to ensure cleanup on deletion.
	finalizerName = "aperture.ai/gpuworkload-finalizer"

	// inferenceImage is the default inference server container image.
	inferenceImage = "aperture-inference:latest"

	// inferencePort is the port the inference server listens on.
	inferencePort = 8080

	// modelVolumeName is the name of the volume mount for model weights.
	modelVolumeName = "model-storage"

	// modelMountPath is the path where model weights are mounted inside the pod.
	modelMountPath = "/models"

	// requeueDelay is the delay before retrying a failed status update.
	requeueDelay = 5 * time.Second

	// containerMemoryLimit is the default memory limit for the inference container.
	containerMemoryLimit = "16Gi"

	// containerCPULimit is the default CPU limit for the inference container.
	containerCPULimit = "4"
)

// GPUWorkloadReconciler reconciles a GPUWorkload object.
type GPUWorkloadReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=aperture.ai,resources=gpuworkloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aperture.ai,resources=gpuworkloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aperture.ai,resources=gpuworkloads/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile performs the reconciliation loop for GPUWorkload resources.
// It ensures that a Deployment and Service exist for each GPUWorkload,
// manages the lifecycle from Pending → Running → Completed/Failed, and
// cleans up owned resources on deletion.
func (r *GPUWorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("gpuworkload", req.NamespacedName)
	logger.Info("Reconciling GPUWorkload")

	// ------------------------------------------------------------------
	// 1. Fetch the GPUWorkload resource
	// ------------------------------------------------------------------
	var workload aperturev1alpha1.GPUWorkload
	if err := r.Get(ctx, req.NamespacedName, &workload); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("GPUWorkload resource not found — likely deleted, skipping")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to fetch GPUWorkload")
		return ctrl.Result{}, err
	}

	// ------------------------------------------------------------------
	// 2. Handle deletion via finalizer
	// ------------------------------------------------------------------
	if !workload.DeletionTimestamp.IsZero() {
		logger.Info("GPUWorkload marked for deletion, running cleanup")
		if controllerutil.ContainsFinalizer(&workload, finalizerName) {
			if err := r.deleteOwnedResources(ctx, &workload); err != nil {
				logger.Error(err, "Failed to delete owned resources during cleanup")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&workload, finalizerName)
			if err := r.Update(ctx, &workload); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			logger.Info("Finalizer removed, cleanup complete")
		}
		return ctrl.Result{}, nil
	}

	// ------------------------------------------------------------------
	// 3. Ensure finalizer is set
	// ------------------------------------------------------------------
	if !controllerutil.ContainsFinalizer(&workload, finalizerName) {
		controllerutil.AddFinalizer(&workload, finalizerName)
		if err := r.Update(ctx, &workload); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Finalizer added")
		return ctrl.Result{Requeue: true}, nil
	}

	// ------------------------------------------------------------------
	// 4. Reconcile Deployment
	// ------------------------------------------------------------------
	deployName := fmt.Sprintf("%s-inference", workload.Name)

	var existingDeploy appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: workload.Namespace}, &existingDeploy)

	if err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to look up inference Deployment")
		return ctrl.Result{}, err
	}

	if errors.IsNotFound(err) {
		logger.Info("Creating inference Deployment", "deployment", deployName)

		deploy := r.buildInferenceDeployment(&workload, deployName)

		if err := controllerutil.SetControllerReference(&workload, deploy, r.Scheme); err != nil {
			logger.Error(err, "Failed to set owner reference on Deployment")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, deploy); err != nil {
			logger.Error(err, "Failed to create inference Deployment")
			return r.updateStatus(ctx, &workload, aperturev1alpha1.PhaseFailed, "",
				fmt.Sprintf("Deployment creation failed: %v", err))
		}

		logger.Info("Inference Deployment created, setting status to Pending")
		return r.updateStatus(ctx, &workload, aperturev1alpha1.PhasePending, deployName,
			"Inference Deployment created, waiting for pods to become ready")
	}

	// ------------------------------------------------------------------
	// 5. Reconcile Service
	// ------------------------------------------------------------------
	svcName := fmt.Sprintf("%s-svc", workload.Name)
	var existingSvc corev1.Service
	svcErr := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: workload.Namespace}, &existingSvc)

	if svcErr != nil && !errors.IsNotFound(svcErr) {
		logger.Error(svcErr, "Failed to look up inference Service")
		return ctrl.Result{}, svcErr
	}

	if errors.IsNotFound(svcErr) {
		logger.Info("Creating inference Service", "service", svcName)
		svc := r.buildInferenceService(&workload, svcName, deployName)

		if err := controllerutil.SetControllerReference(&workload, svc, r.Scheme); err != nil {
			logger.Error(err, "Failed to set owner reference on Service")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, svc); err != nil {
			logger.Error(err, "Failed to create inference Service")
			// Non-fatal: Deployment still works, log and continue.
		} else {
			logger.Info("Inference Service created", "service", svcName)
		}
	}

	// ------------------------------------------------------------------
	// 6. Update status based on Deployment readiness
	// ------------------------------------------------------------------
	readyReplicas := existingDeploy.Status.ReadyReplicas
	desiredReplicas := int32(1)
	if existingDeploy.Spec.Replicas != nil {
		desiredReplicas = *existingDeploy.Spec.Replicas
	}

	switch {
	case readyReplicas >= desiredReplicas && desiredReplicas > 0:
		if workload.Status.Phase != aperturev1alpha1.PhaseRunning {
			logger.Info("Deployment is ready, updating workload status")
			return r.updateStatus(ctx, &workload, aperturev1alpha1.PhaseRunning, deployName,
				fmt.Sprintf("Inference server running (%d/%d replicas ready)", readyReplicas, desiredReplicas))
		}
	case existingDeploy.Status.UnavailableReplicas > 0:
		// Check for crash loops in conditions.
		for _, cond := range existingDeploy.Status.Conditions {
			if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
				return r.updateStatus(ctx, &workload, aperturev1alpha1.PhaseFailed, deployName,
					fmt.Sprintf("Deployment replica failure: %s", cond.Message))
			}
		}
		if workload.Status.Phase != aperturev1alpha1.PhasePending {
			return r.updateStatus(ctx, &workload, aperturev1alpha1.PhasePending, deployName,
				"Inference pods are starting up")
		}
	default:
		if workload.Status.Phase != aperturev1alpha1.PhasePending {
			return r.updateStatus(ctx, &workload, aperturev1alpha1.PhasePending, deployName,
				"Waiting for inference pods to schedule")
		}
	}

	return ctrl.Result{}, nil
}

// buildInferenceDeployment constructs the Deployment spec for the vLLM inference server.
func (r *GPUWorkloadReconciler) buildInferenceDeployment(
	workload *aperturev1alpha1.GPUWorkload, deployName string,
) *appsv1.Deployment {
	replicas := int32(1)
	runAsNonRoot := true
	runAsUser := int64(1000)
	fsGroup := int64(1000)
	readOnlyRoot := true

	useGPU := "true"
	if workload.Spec.GPU == 0 {
		useGPU = "false"
	}

	resourceLimits := corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("1Gi"),
		corev1.ResourceCPU:    resource.MustParse("1"),
	}
	resourceRequests := corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("512Mi"),
		corev1.ResourceCPU:    resource.MustParse("250m"),
	}

	if workload.Spec.GPU > 0 {
		gpuQuantity := resource.MustParse(fmt.Sprintf("%d", workload.Spec.GPU))
		resourceLimits["nvidia.com/gpu"] = gpuQuantity
		resourceRequests["nvidia.com/gpu"] = gpuQuantity
	}

	labels := map[string]string{
		"app":                        "aperture-inference",
		"aperture.ai/workload":       workload.Name,
		"aperture.ai/partition-mode": workload.Spec.PartitionMode,
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: workload.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                  "aperture-inference",
					"aperture.ai/workload": workload.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					// Security: run as non-root with restricted capabilities.
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						FSGroup:      &fsGroup,
					},
					Containers: []corev1.Container{
						{
							Name:  "inference",
							Image: inferenceImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: int32(inferencePort),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "MODEL_PATH",
									Value: workload.Spec.ModelName,
								},
								{
									Name:  "TOKEN_QUOTA",
									Value: fmt.Sprintf("%d", workload.Spec.TokenQuota),
								},
								{
									Name:  "KV_CACHE_GB",
									Value: fmt.Sprintf("%d", workload.Spec.KVCacheGB),
								},
								{
									Name:  "GPU_PARTITION_MODE",
									Value: workload.Spec.PartitionMode,
								},
								{
									Name:  "USE_GPU",
									Value: useGPU,
								},
								// Torch reads /etc/passwd to get username for cache dirs.
								// runAsUser=1000 has no passwd entry, so we pin dirs to /tmp.
								{
									Name:  "HOME",
									Value: "/tmp",
								},
								{
									Name:  "TORCHINDUCTOR_CACHE_DIR",
									Value: "/tmp/torchinductor",
								},
								{
									Name:  "TORCH_HOME",
									Value: "/tmp/torch",
								},
								{
									Name:  "HF_HOME",
									Value: "/tmp/huggingface",
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits:   resourceLimits,
								Requests: resourceRequests,
							},
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: &readOnlyRoot,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      modelVolumeName,
									MountPath: modelMountPath,
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(inferencePort),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    6,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(inferencePort),
									},
								},
								InitialDelaySeconds: 60,
								PeriodSeconds:       15,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
						},
					},
					// Init container: pre-download model weights to shared PVC.
					InitContainers: []corev1.Container{
						{
							Name:  "model-preloader",
							Image: inferenceImage,
							Command: []string{
								"python", "/app/model_preloader.py",
							},
							Env: []corev1.EnvVar{
								{
									Name:  "MODEL_PATH",
									Value: workload.Spec.ModelName,
								},
								{
									Name:  "CACHE_DIR",
									Value: modelMountPath,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      modelVolumeName,
									MountPath: modelMountPath,
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
									corev1.ResourceCPU:    resource.MustParse("2"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("512Mi"),
									corev1.ResourceCPU:    resource.MustParse("500m"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: modelVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "aperture-model-cache",
								},
							},
						},
						{
							// Writable /tmp for read-only root filesystem.
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	return deploy
}

// buildInferenceService constructs a ClusterIP Service for the inference Deployment.
func (r *GPUWorkloadReconciler) buildInferenceService(
	workload *aperturev1alpha1.GPUWorkload, svcName, deployName string,
) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: workload.Namespace,
			Labels: map[string]string{
				"app":                  "aperture-inference",
				"aperture.ai/workload": workload.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app":                  "aperture-inference",
				"aperture.ai/workload": workload.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       int32(inferencePort),
					TargetPort: intstr.FromInt(inferencePort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// deleteOwnedResources removes the Deployment and Service associated with this workload.
func (r *GPUWorkloadReconciler) deleteOwnedResources(ctx context.Context, workload *aperturev1alpha1.GPUWorkload) error {
	logger := log.FromContext(ctx)

	// Delete Deployment.
	deployName := fmt.Sprintf("%s-inference", workload.Name)
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: workload.Namespace}, &deploy); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get owned Deployment %s: %w", deployName, err)
		}
		logger.Info("Owned Deployment already deleted", "deployment", deployName)
	} else {
		if err := r.Delete(ctx, &deploy); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete owned Deployment %s: %w", deployName, err)
		}
		logger.Info("Deleted owned Deployment", "deployment", deployName)
	}

	// Delete Service.
	svcName := fmt.Sprintf("%s-svc", workload.Name)
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: workload.Namespace}, &svc); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get owned Service %s: %w", svcName, err)
		}
		logger.Info("Owned Service already deleted", "service", svcName)
	} else {
		if err := r.Delete(ctx, &svc); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete owned Service %s: %w", svcName, err)
		}
		logger.Info("Deleted owned Service", "service", svcName)
	}

	return nil
}

// updateStatus patches the GPUWorkload status subresource and returns a reconcile result.
func (r *GPUWorkloadReconciler) updateStatus(
	ctx context.Context,
	workload *aperturev1alpha1.GPUWorkload,
	phase aperturev1alpha1.GPUWorkloadPhase,
	podName string,
	message string,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	workload.Status.Phase = phase
	workload.Status.PodName = podName
	workload.Status.Message = message

	if err := r.Status().Update(ctx, workload); err != nil {
		logger.Error(err, "Failed to update GPUWorkload status",
			"phase", phase, "podName", podName)
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	logger.Info("Status updated", "phase", phase, "podName", podName, "message", message)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager and configures
// watches for GPUWorkload resources, owned Deployments, and owned Services.
func (r *GPUWorkloadReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aperturev1alpha1.GPUWorkload{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
