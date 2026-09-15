// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package liquidserver_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/go-api-declarations/liquid"
	. "go.xyrillian.de/gg/option"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
	internalkubernetes "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
	"github.com/sap-cloud-infrastructure/persephone/internal/liquidserver"
)

var _ = Describe("Logic", func() {
	const testRegion = "test-region"

	var (
		logic       *liquidserver.Logic
		projectUUID string
		projectNS   string
		serviceInfo liquid.ServiceInfo
	)

	BeforeEach(func() {
		var err error
		logic, err = liquidserver.NewLogic(ctx, testRegion, testClient, logf.Log)
		Expect(err).NotTo(HaveOccurred())

		// A fresh UUID per test prevents namespace collisions: Kubernetes deletes namespaces
		// asynchronously, so re-using the same name across tests causes "already exists" errors.
		projectUUID = testProjectUUID()
		projectNS = internalkubernetes.GetGardenerProjectNamespaceName(testRegion, projectUUID)
		serviceInfo = liquid.ServiceInfo{Version: 1}

		By("Create project namespace")
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: projectNS}}
		Expect(testClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() {
			Expect(testClient.Delete(ctx, ns)).To(Succeed())
		})
	})

	// createResourceQuota simulates the webhook provisioning a ResourceQuota in the project namespace.
	createResourceQuota := func(quota string) *corev1.ResourceQuota {
		rq := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      constants.ResourceQuotaName,
				Namespace: projectNS,
			},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					constants.ShootResourceQuotaKey: resource.MustParse(quota),
				},
			},
		}
		ExpectWithOffset(1, testClient.Create(ctx, rq)).To(Succeed())
		return rq
	}

	Describe("SetQuota", func() {
		Context("with a provisioned ResourceQuota", func() {
			BeforeEach(func() {
				createResourceQuota("1")
			})

			It("updates the ResourceQuota to the requested spec.hard value", func() {
				req := liquid.ServiceQuotaRequest{
					Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{
						"clusters": {Quota: 10},
					},
				}
				Expect(logic.SetQuota(ctx, projectUUID, req, serviceInfo)).To(Succeed())

				rq := &corev1.ResourceQuota{}
				Expect(testClient.Get(ctx, client.ObjectKey{Name: constants.ResourceQuotaName, Namespace: projectNS}, rq)).To(Succeed())
				Expect(rq.Spec.Hard[constants.ShootResourceQuotaKey]).To(Equal(resource.MustParse("10")))
			})

			It("updates an existing ResourceQuota to the new quota value", func() {
				req := liquid.ServiceQuotaRequest{
					Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{
						"clusters": {Quota: 5},
					},
				}
				Expect(logic.SetQuota(ctx, projectUUID, req, serviceInfo)).To(Succeed())

				req.Resources["clusters"] = liquid.ResourceQuotaRequest{Quota: 20}
				Expect(logic.SetQuota(ctx, projectUUID, req, serviceInfo)).To(Succeed())

				rq := &corev1.ResourceQuota{}
				Expect(testClient.Get(ctx, client.ObjectKey{Name: constants.ResourceQuotaName, Namespace: projectNS}, rq)).To(Succeed())
				Expect(rq.Spec.Hard[constants.ShootResourceQuotaKey]).To(Equal(resource.MustParse("20")))
			})
		})

		It("returns 400 when the request does not include the 'clusters' resource", func() {
			req := liquid.ServiceQuotaRequest{
				Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{},
			}
			Expect(logic.SetQuota(ctx, projectUUID, req, serviceInfo)).To(MatchError(ContainSubstring("request must include the 'clusters' resource")))
		})

		It("returns HTTP 422 when the namespace exists but the ResourceQuota is missing", func() {
			req := liquid.ServiceQuotaRequest{
				Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{
					"clusters": {Quota: 10},
				},
			}
			Expect(logic.SetQuota(ctx, projectUUID, req, serviceInfo)).To(MatchError(ContainSubstring("operator/webhook provisioning contract was violated")))
		})
	})

	Describe("ScanUsage", func() {
		It("returns the correct usage when status.used is populated", func() {
			rq := createResourceQuota("10")

			By("Simulate kube-controller-manager populating status.used")
			rq.Status.Used = corev1.ResourceList{
				constants.ShootResourceQuotaKey: resource.MustParse("3"),
			}
			Expect(testClient.Status().Update(ctx, rq)).To(Succeed())

			req := liquid.ServiceUsageRequest{}
			report, err := logic.ScanUsage(ctx, projectUUID, req, serviceInfo)
			Expect(err).NotTo(HaveOccurred())

			azReport := report.Resources["clusters"].PerAZ[liquid.AvailabilityZoneAny]
			Expect(azReport).NotTo(BeNil())
			Expect(azReport.Usage).To(Equal(uint64(3)))
			Expect(report.Resources["clusters"].Quota).To(Equal(Some[int64](10)))
		})

		It("returns an error when the namespace exists but the ResourceQuota is missing", func() {
			req := liquid.ServiceUsageRequest{}
			_, err := logic.ScanUsage(ctx, projectUUID, req, serviceInfo)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(constants.ResourceQuotaName))
		})

		It("returns zero usage when status.used does not contain the shoot key", func() {
			createResourceQuota("10")

			req := liquid.ServiceUsageRequest{}
			report, err := logic.ScanUsage(ctx, projectUUID, req, serviceInfo)
			Expect(err).NotTo(HaveOccurred())

			azReport := report.Resources["clusters"].PerAZ[liquid.AvailabilityZoneAny]
			Expect(azReport).NotTo(BeNil())
			Expect(azReport.Usage).To(Equal(uint64(0)))
			Expect(report.Resources["clusters"].Quota).To(Equal(Some[int64](10)))
		})
	})

	Describe("SetQuota + ScanUsage round-trip", func() {
		It("reflects the quota set by SetQuota in a subsequent ScanUsage report", func() {
			createResourceQuota("1")

			By("Set quota via SetQuota")
			setReq := liquid.ServiceQuotaRequest{
				Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{
					"clusters": {Quota: 7},
				},
			}
			Expect(logic.SetQuota(ctx, projectUUID, setReq, serviceInfo)).To(Succeed())

			By("Simulate status.used population by kube-controller-manager")
			rq := &corev1.ResourceQuota{}
			Expect(testClient.Get(ctx, client.ObjectKey{Name: constants.ResourceQuotaName, Namespace: projectNS}, rq)).To(Succeed())
			rq.Status.Used = corev1.ResourceList{
				constants.ShootResourceQuotaKey: resource.MustParse("2"),
			}
			Expect(testClient.Status().Update(ctx, rq)).To(Succeed())

			By("Verify ScanUsage reflects the quota value")
			usageReq := liquid.ServiceUsageRequest{}
			report, err := logic.ScanUsage(ctx, projectUUID, usageReq, serviceInfo)
			Expect(err).NotTo(HaveOccurred())

			azReport := report.Resources["clusters"].PerAZ[liquid.AvailabilityZoneAny]
			Expect(azReport).NotTo(BeNil())
			Expect(azReport.Usage).To(Equal(uint64(2)))
			Expect(report.Resources["clusters"].Quota).To(Equal(Some[int64](7)))
		})
	})

	Describe("when the project namespace does not exist", func() {
		var (
			logic2       *liquidserver.Logic
			projectUUID2 string
			serviceInfo2 liquid.ServiceInfo
		)

		BeforeEach(func() {
			var err error
			logic2, err = liquidserver.NewLogic(ctx, testRegion, testClient, logf.Log)
			Expect(err).NotTo(HaveOccurred())

			projectUUID2 = testProjectUUID()
			serviceInfo2 = liquid.ServiceInfo{Version: 1}
		})

		It("ScanUsage returns Forbidden=true without error", func() {
			req := liquid.ServiceUsageRequest{}
			report, err := logic2.ScanUsage(ctx, projectUUID2, req, serviceInfo2)
			Expect(err).NotTo(HaveOccurred())

			Expect(report.Resources["clusters"].Forbidden).To(BeTrue())
			azReport := report.Resources["clusters"].PerAZ[liquid.AvailabilityZoneAny]
			Expect(azReport).NotTo(BeNil())
			Expect(azReport.Usage).To(Equal(uint64(0)))
			Expect(report.Resources["clusters"].Quota).To(Equal(Some[int64](0)))
		})

		It("SetQuota returns HTTP 422 when the namespace does not exist", func() {
			req := liquid.ServiceQuotaRequest{
				Resources: map[liquid.ResourceName]liquid.ResourceQuotaRequest{
					"clusters": {Quota: 10},
				},
			}
			Expect(logic2.SetQuota(ctx, projectUUID2, req, serviceInfo2)).To(MatchError(ContainSubstring("Forbidden contract was seemingly not honored")))
		})
	})
})
