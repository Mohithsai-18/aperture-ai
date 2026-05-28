package v1alpha1

import (
	"testing"
)

func TestValidateCreate_ValidSpec(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "facebook/opt-125m",
			GPU:           2,
			PartitionMode: "MPS",
			TokenQuota:    50000,
			KVCacheGB:     4,
		},
	}
	workload.Name = "test-workload"

	warnings, err := workload.ValidateCreate()
	if err != nil {
		t.Errorf("expected no error for valid spec, got: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}
}

func TestValidateCreate_EmptyModelName(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    1000,
			KVCacheGB:     2,
		},
	}
	workload.Name = "test-empty-model"

	_, err := workload.ValidateCreate()
	if err == nil {
		t.Error("expected error for empty modelName, got nil")
	}
}

func TestValidateCreate_GPUOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		gpu  int
	}{
		{"gpu_negative", -1},
		{"gpu_over_max", 9},
		{"gpu_way_over", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workload := &GPUWorkload{
				Spec: GPUWorkloadSpec{
					ModelName:     "test-model",
					GPU:           tt.gpu,
					PartitionMode: "MIG",
					TokenQuota:    1000,
					KVCacheGB:     1,
				},
			}
			workload.Name = "test-gpu-range"

			_, err := workload.ValidateCreate()
			if err == nil {
				t.Errorf("expected error for gpu=%d, got nil", tt.gpu)
			}
		})
	}
}

func TestValidateCreate_GPUValidRange(t *testing.T) {
	for gpu := 0; gpu <= 8; gpu++ {
		workload := &GPUWorkload{
			Spec: GPUWorkloadSpec{
				ModelName:     "test-model",
				GPU:           gpu,
				PartitionMode: "MPS",
				TokenQuota:    1000,
				KVCacheGB:     1,
			},
		}
		workload.Name = "test-gpu-valid"

		_, err := workload.ValidateCreate()
		if err != nil {
			t.Errorf("expected no error for gpu=%d, got: %v", gpu, err)
		}
	}
}

func TestValidateCreate_InvalidPartitionMode(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{"empty", ""},
		{"lowercase_mps", "mps"},
		{"invalid_mode", "TIME_SHARING"},
		{"typo", "MPSS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workload := &GPUWorkload{
				Spec: GPUWorkloadSpec{
					ModelName:     "test-model",
					GPU:           1,
					PartitionMode: tt.mode,
					TokenQuota:    1000,
					KVCacheGB:     1,
				},
			}
			workload.Name = "test-partition"

			_, err := workload.ValidateCreate()
			if err == nil {
				t.Errorf("expected error for partitionMode=%q, got nil", tt.mode)
			}
		})
	}
}

func TestValidateCreate_ValidPartitionModes(t *testing.T) {
	for _, mode := range []string{"MPS", "MIG"} {
		workload := &GPUWorkload{
			Spec: GPUWorkloadSpec{
				ModelName:     "test-model",
				GPU:           1,
				PartitionMode: mode,
				TokenQuota:    1000,
				KVCacheGB:     1,
			},
		}
		workload.Name = "test-partition-valid"

		_, err := workload.ValidateCreate()
		if err != nil {
			t.Errorf("expected no error for partitionMode=%q, got: %v", mode, err)
		}
	}
}

func TestValidateCreate_TokenQuotaExceeded(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "test-model",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    100001,
			KVCacheGB:     1,
		},
	}
	workload.Name = "test-quota-exceeded"

	_, err := workload.ValidateCreate()
	if err == nil {
		t.Error("expected error for tokenQuota=100001, got nil")
	}
}

func TestValidateCreate_TokenQuotaBoundary(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "test-model",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    100000,
			KVCacheGB:     1,
		},
	}
	workload.Name = "test-quota-boundary"

	_, err := workload.ValidateCreate()
	if err != nil {
		t.Errorf("expected no error for tokenQuota=100000, got: %v", err)
	}
}

func TestValidateCreate_TokenQuotaZero(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "test-model",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    0,
			KVCacheGB:     1,
		},
	}
	workload.Name = "test-quota-zero"

	_, err := workload.ValidateCreate()
	if err == nil {
		t.Error("expected error for tokenQuota=0, got nil")
	}
}

func TestValidateCreate_KVCacheGBZero(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "test-model",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    1000,
			KVCacheGB:     0,
		},
	}
	workload.Name = "test-kvcache-zero"

	_, err := workload.ValidateCreate()
	if err == nil {
		t.Error("expected error for kvCacheGB=0, got nil")
	}
}

func TestValidateCreate_MultipleErrors(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "",
			GPU:           -1,
			PartitionMode: "INVALID",
			TokenQuota:    200000,
			KVCacheGB:     0,
		},
	}
	workload.Name = "test-multiple-errors"

	_, err := workload.ValidateCreate()
	if err == nil {
		t.Error("expected error for multiple invalid fields, got nil")
	}
}

func TestValidateDelete_AlwaysSucceeds(t *testing.T) {
	workload := &GPUWorkload{}
	workload.Name = "test-delete"

	_, err := workload.ValidateDelete()
	if err != nil {
		t.Errorf("expected no error on delete, got: %v", err)
	}
}

func TestValidateUpdate_SameAsCreate(t *testing.T) {
	workload := &GPUWorkload{
		Spec: GPUWorkloadSpec{
			ModelName:     "",
			GPU:           1,
			PartitionMode: "MPS",
			TokenQuota:    1000,
			KVCacheGB:     1,
		},
	}
	workload.Name = "test-update"

	_, err := workload.ValidateUpdate(nil)
	if err == nil {
		t.Error("expected error for empty modelName on update, got nil")
	}
}
