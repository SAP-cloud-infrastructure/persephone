// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/sapcc/go-api-declarations/liquid"
	"github.com/sapcc/go-bits/respondwith"
	. "go.xyrillian.de/gg/option"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	internalkubernetes "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

// Logic implements the liquidapi.Logic interface for Persephone.
// It provides quota, usage, and capacity reporting for Gardener shoot clusters.
type Logic struct {
	log        logr.Logger
	region     string
	kubeClient client.Client
}

// NewLogic creates a new Logic instance.
func NewLogic(ctx context.Context, region string, kubeClient client.Client, log logr.Logger) (*Logic, error) {
	return &Logic{
		log:        log,
		region:     region,
		kubeClient: kubeClient,
	}, nil
}

// Init is called by liquidapi.Run() to initialize the logic with OpenStack credentials.
// Since the liquid-apiserver does not use an OpenStack client, this is a no-op.
func (l *Logic) Init(ctx context.Context, provider *gophercloud.ProviderClient, eo gophercloud.EndpointOpts) error {
	l.log.Info("Liquid API server initialized", "region", l.region)
	return nil
}

// BuildServiceInfo returns metadata about the service and its resources.
func (l *Logic) BuildServiceInfo(ctx context.Context) (liquid.ServiceInfo, error) {
	return liquid.ServiceInfo{
		Version:     time.Now().Unix(),
		DisplayName: "Managed Kubernetes clusters on SCI",
		Resources: map[liquid.ResourceName]liquid.ResourceInfo{
			"clusters": {
				DisplayName: "Managed Kubernetes clusters",
				Unit:        liquid.UnitPiece,
				Topology:    liquid.FlatTopology,
				HasQuota:    true,
				HasCapacity: false,
			},
		},
	}, nil
}

// ScanUsage reports actual cluster usage for a project by reading status.used from the cached ResourceQuota.
//
// CONTRACT with the namespace provisioning side (internal/kubernetes/gardener.go, docs/liquid-apiserver.md):
// Namespace existence is the signal that a project is active. If the namespace does not exist, this returns
// Forbidden: true so Limes skips SetQuota for that project. If the namespace exists, the ResourceQuota must
// also exist (guaranteed by ReconcileGardenerProjectResources, which creates both atomically); a missing
// ResourceQuota when the namespace is present is treated as a provisioning contract violation and returns an error.
func (l *Logic) ScanUsage(ctx context.Context, projectUUID string, req liquid.ServiceUsageRequest, serviceInfo liquid.ServiceInfo) (liquid.ServiceUsageReport, error) {
	namespace := internalkubernetes.GetGardenerProjectNamespaceName(l.region, projectUUID)

	ns := &corev1.Namespace{}
	if err := l.kubeClient.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return liquid.ServiceUsageReport{
				InfoVersion: serviceInfo.Version,
				Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
					"clusters": {
						Forbidden: true,
						Quota:     Some[int64](0),
						PerAZ:     liquid.InAnyAZ(liquid.AZResourceUsageReport{Usage: 0}),
					},
				},
			}, nil
		}
		return liquid.ServiceUsageReport{}, fmt.Errorf("failed checking namespace %s: %w", namespace, err)
	}

	rq := &corev1.ResourceQuota{}
	if err := l.kubeClient.Get(ctx, client.ObjectKey{Name: constants.ResourceQuotaName, Namespace: namespace}, rq); err != nil {
		return liquid.ServiceUsageReport{}, fmt.Errorf("namespace %s exists but ResourceQuota %s not found: %w", namespace, constants.ResourceQuotaName, err)
	}

	var usage uint64
	if usedQty, ok := rq.Status.Used[constants.ShootResourceQuotaKey]; ok {
		usage = uint64(usedQty.Value()) //nolint:gosec // usage values from Kubernetes are always within uint64 range
	}

	quota := Some[int64](-1)
	if q, ok := rq.Spec.Hard[constants.ShootResourceQuotaKey]; ok {
		quota = Some(q.Value())
	}

	l.log.V(1).Info("ScanUsage called", "projectUUID", projectUUID, "region", l.region, "usage", usage, "quota", quota)

	return liquid.ServiceUsageReport{
		InfoVersion: serviceInfo.Version,
		Resources: map[liquid.ResourceName]*liquid.ResourceUsageReport{
			"clusters": {
				Quota: quota,
				PerAZ: liquid.InAnyAZ(liquid.AZResourceUsageReport{Usage: usage}),
			},
		},
	}, nil
}

// SetQuota reconciles a v1/ResourceQuota in the project namespace on the virtual garden cluster.
func (l *Logic) SetQuota(ctx context.Context, projectUUID string, req liquid.ServiceQuotaRequest, serviceInfo liquid.ServiceInfo) error {
	quotaReq, exists := req.Resources["clusters"]
	if !exists {
		return respondwith.CustomStatus(http.StatusBadRequest, errors.New("request must include the 'clusters' resource"))
	}

	namespace := internalkubernetes.GetGardenerProjectNamespaceName(l.region, projectUUID)

	ns := &corev1.Namespace{}
	if err := l.kubeClient.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			return respondwith.CustomStatus(http.StatusUnprocessableEntity,
				fmt.Errorf("SetQuota called for project %s but namespace %s does not exist; the Forbidden contract was seemingly not honored", projectUUID, namespace))
		}
		return fmt.Errorf("failed checking namespace %s: %w", namespace, err)
	}

	existingRQ := &corev1.ResourceQuota{}
	if err := l.kubeClient.Get(ctx, client.ObjectKey{Name: constants.ResourceQuotaName, Namespace: namespace}, existingRQ); err != nil {
		if apierrors.IsNotFound(err) {
			return respondwith.CustomStatus(http.StatusUnprocessableEntity,
				fmt.Errorf("SetQuota called for project %s: namespace %s exists but ResourceQuota %s does not; the operator/webhook provisioning contract was violated", projectUUID, namespace, constants.ResourceQuotaName))
		}
		return fmt.Errorf("failed checking ResourceQuota in namespace %s: %w", namespace, err)
	}

	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ResourceQuotaName,
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrPatch(ctx, l.kubeClient, rq, func() error {
		if rq.Spec.Hard == nil {
			rq.Spec.Hard = make(corev1.ResourceList)
		}
		quota := min(quotaReq.Quota, math.MaxInt64)
		rq.Spec.Hard[constants.ShootResourceQuotaKey] = *resource.NewQuantity(int64(quota), resource.DecimalSI)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed reconciling ResourceQuota for project %s in namespace %s: %w", projectUUID, namespace, err)
	}

	l.log.Info("Reconciled ResourceQuota", "projectUUID", projectUUID, "namespace", namespace, "quota", quotaReq.Quota)
	return nil
}

// ScanCapacity is a no-op because the "clusters" resource does not have a meaningful
// hardware capacity ceiling — it is governed by quota only (HasCapacity: false).
func (l *Logic) ScanCapacity(ctx context.Context, req liquid.ServiceCapacityRequest, serviceInfo liquid.ServiceInfo) (liquid.ServiceCapacityReport, error) {
	return liquid.ServiceCapacityReport{InfoVersion: serviceInfo.Version}, nil
}

// ReviewCommitmentChange validates commitment change requests.
// This is a stub implementation that accepts all commitment changes.
func (l *Logic) ReviewCommitmentChange(ctx context.Context, req liquid.CommitmentChangeRequest, serviceInfo liquid.ServiceInfo) (liquid.CommitmentChangeResponse, error) {
	l.log.V(1).Info("ReviewCommitmentChange called")

	// Stub: accept all commitment changes
	return liquid.CommitmentChangeResponse{}, nil
}

// Ensure Logic implements liquidapi.Logic interface at compile time.
var _ interface {
	Init(context.Context, *gophercloud.ProviderClient, gophercloud.EndpointOpts) error
	BuildServiceInfo(context.Context) (liquid.ServiceInfo, error)
	ScanUsage(context.Context, string, liquid.ServiceUsageRequest, liquid.ServiceInfo) (liquid.ServiceUsageReport, error)
	SetQuota(context.Context, string, liquid.ServiceQuotaRequest, liquid.ServiceInfo) error
	ScanCapacity(context.Context, liquid.ServiceCapacityRequest, liquid.ServiceInfo) (liquid.ServiceCapacityReport, error)
	ReviewCommitmentChange(context.Context, liquid.CommitmentChangeRequest, liquid.ServiceInfo) (liquid.CommitmentChangeResponse, error)
} = (*Logic)(nil)
