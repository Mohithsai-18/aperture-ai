package controllers

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aperturev1alpha1 "github.com/aperture-ai/operator/api/v1alpha1"
)

func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = aperturev1alpha1.AddToScheme(s)
	return s
}

func TestGPUWorkloadReconciler_CreatesDeploymentAndService(t *testing.T) {
	s := setupScheme()

	workload := &aperturev1alpha1.GPUWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workload",
			Namespace: "default",
		},
		Spec: aperturev1alpha1.GPUWorkloadSpec{
			ModelName:     "facebook/opt-125m",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    50000,
			KVCacheGB:     4,
		},
	}

	// Create a fake client to mock API server interactions.
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(workload).
		WithStatusSubresource(workload).
		Build()

	r := &GPUWorkloadReconciler{
		Client: cl,
		Scheme: s,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-workload",
			Namespace: "default",
		},
	}

	ctx := context.TODO()

	// 1. First Reconcile: Should add finalizer
	res, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("First reconcile failed: %v", err)
	}
	if !res.Requeue {
		t.Errorf("Expected requeue after adding finalizer")
	}

	// 2. Second Reconcile: Should create Deployment and Service
	res, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Second reconcile failed: %v", err)
	}

	// Verify Deployment
	var deploy appsv1.Deployment
	err = cl.Get(ctx, types.NamespacedName{Name: "test-workload-inference", Namespace: "default"}, &deploy)
	if err != nil {
		t.Fatalf("Expected deployment to be created, but got error: %v", err)
	}

	if deploy.Spec.Template.Spec.Containers[0].Image != "aperture-inference:latest" {
		t.Errorf("Unexpected container image: %s", deploy.Spec.Template.Spec.Containers[0].Image)
	}

	// Verify Service
	var svc corev1.Service
	err = cl.Get(ctx, types.NamespacedName{Name: "test-workload-svc", Namespace: "default"}, &svc)
	if err != nil {
		t.Fatalf("Expected service to be created, but got error: %v", err)
	}

	// Verify Workload Status
	updatedWorkload := &aperturev1alpha1.GPUWorkload{}
	err = cl.Get(ctx, req.NamespacedName, updatedWorkload)
	if err != nil {
		t.Fatalf("Failed to fetch updated workload: %v", err)
	}

	if updatedWorkload.Status.Phase != aperturev1alpha1.PhasePending {
		t.Errorf("Expected PhasePending, got %s", updatedWorkload.Status.Phase)
	}
	if updatedWorkload.Status.PodName != "test-workload-inference" {
		t.Errorf("Expected PodName 'test-workload-inference', got '%s'", updatedWorkload.Status.PodName)
	}
}
