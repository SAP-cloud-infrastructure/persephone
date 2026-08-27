// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidserver

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/liquid"
	"go.xyrillian.de/gg/assert"
	. "go.xyrillian.de/gg/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	internalkubernetes "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

func TestLogic_BuildServiceInfo(t *testing.T) {
	// Create a minimal Logic instance for testing
	logic := &Logic{}

	serviceInfo, err := logic.BuildServiceInfo(t.Context())
	if err != nil {
		t.Fatalf("BuildServiceInfo failed: %v", err)
	}

	// Verify service info structure
	if serviceInfo.Version == 0 {
		t.Error("Expected non-zero version")
	}

	// Verify service display name
	if serviceInfo.DisplayName != "Managed Kubernetes clusters on SCI" {
		t.Errorf("Expected DisplayName %q, got %q", "Managed Kubernetes clusters on SCI", serviceInfo.DisplayName)
	}

	// Verify clusters resource exists
	clusterResource, exists := serviceInfo.Resources["clusters"]
	if !exists {
		t.Fatal("Expected 'clusters' resource in service info")
	}

	assert.Equal(t, clusterResource, liquid.ResourceInfo{
		DisplayName: "Managed Kubernetes clusters",
		Unit:        liquid.UnitPiece,
		Topology:    liquid.FlatTopology,
		HasQuota:    true,
		HasCapacity: false,
	})
}

func TestLogic_ScanCapacity(t *testing.T) {
	logic := &Logic{
		region: "test-region",
	}

	serviceInfo := liquid.ServiceInfo{Version: 1}
	req := liquid.ServiceCapacityRequest{}

	report, err := logic.ScanCapacity(t.Context(), req, serviceInfo)
	if err != nil {
		t.Fatalf("ScanCapacity failed: %v", err)
	}

	// No capacity resources are reported since HasCapacity is false for all resources.
	if report.InfoVersion != serviceInfo.Version {
		t.Errorf("Expected InfoVersion %d, got %d", serviceInfo.Version, report.InfoVersion)
	}
	if len(report.Resources) != 0 {
		t.Errorf("Expected no resources in capacity report, got %d", len(report.Resources))
	}
}

func TestLogic_ReviewCommitmentChange(t *testing.T) {
	logic := &Logic{}

	serviceInfo := liquid.ServiceInfo{Version: 1}
	req := liquid.CommitmentChangeRequest{}

	// Should accept all commitment changes (stub implementation)
	response, err := logic.ReviewCommitmentChange(t.Context(), req, serviceInfo)
	if err != nil {
		t.Fatalf("ReviewCommitmentChange failed: %v", err)
	}

	// For stub, response should be empty
	_ = response
}

func TestLogic_ScanUsage_KeyPresent(t *testing.T) {
	const (
		region      = "test-region"
		projectUUID = "abc123"
	)
	namespace := internalkubernetes.GetGardenerProjectNamespaceName(region, projectUUID)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ResourceQuotaName,
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				constants.ShootResourceQuotaKey: resource.MustParse("7"),
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{
				constants.ShootResourceQuotaKey: resource.MustParse("3"),
			},
		},
	}

	fakeClient := fakeclient.NewClientBuilder().
		WithScheme(internalkubernetes.GardenerScheme).
		WithObjects(ns, rq).
		WithStatusSubresource(rq).
		Build()

	logic := &Logic{log: logr.Discard(), region: region, kubeClient: fakeClient}
	serviceInfo := liquid.ServiceInfo{Version: 42}

	report, err := logic.ScanUsage(t.Context(), projectUUID, liquid.ServiceUsageRequest{}, serviceInfo)
	if err != nil {
		t.Fatalf("ScanUsage failed: %v", err)
	}

	assert.Equal(t, report, liquid.ServiceUsageReport{
		InfoVersion: 42,
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"clusters": {
				Quota: Some[int64](7),
				PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{Usage: 3}),
			},
		},
	})
}

func TestLogic_ScanUsage_KeyAbsent(t *testing.T) {
	const (
		region      = "test-region"
		projectUUID = "abc123"
	)
	namespace := internalkubernetes.GetGardenerProjectNamespaceName(region, projectUUID)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}

	// ResourceQuota exists but status.used has no shoot key (zero-shoot steady state).
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ResourceQuotaName,
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				constants.ShootResourceQuotaKey: resource.MustParse("5"),
			},
		},
	}

	fakeClient := fakeclient.NewClientBuilder().
		WithScheme(internalkubernetes.GardenerScheme).
		WithObjects(ns, rq).
		Build()

	logic := &Logic{log: logr.Discard(), region: region, kubeClient: fakeClient}
	serviceInfo := liquid.ServiceInfo{Version: 42}

	report, err := logic.ScanUsage(t.Context(), projectUUID, liquid.ServiceUsageRequest{}, serviceInfo)
	if err != nil {
		t.Fatalf("ScanUsage should not error on missing status.used key: %v", err)
	}

	assert.Equal(t, report, liquid.ServiceUsageReport{
		InfoVersion: 42,
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"clusters": {
				Quota: Some[int64](5),
				PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{Usage: 0}),
			},
		},
	})
}

func TestLogic_ScanUsage_NotFound(t *testing.T) {
	const (
		region      = "test-region"
		projectUUID = "abc123"
	)

	fakeClient := fakeclient.NewClientBuilder().
		WithScheme(internalkubernetes.GardenerScheme).
		Build()

	logic := &Logic{log: logr.Discard(), region: region, kubeClient: fakeClient}
	serviceInfo := liquid.ServiceInfo{Version: 42}

	report, err := logic.ScanUsage(t.Context(), projectUUID, liquid.ServiceUsageRequest{}, serviceInfo)
	if err != nil {
		t.Fatalf("ScanUsage should not error when namespace not found: %v", err)
	}

	assert.Equal(t, report, liquid.ServiceUsageReport{
		InfoVersion: 42,
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"clusters": {
				Forbidden: true,
				Quota:     Some[int64](0),
				PerAZ:     liquid.InAnyAZ(liquid.AZResourceUsageReport{Usage: 0}),
			},
		},
	})
}
