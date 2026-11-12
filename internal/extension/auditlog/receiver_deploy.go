// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	receiverImage     = "keppel.eu-de-1.cloud.sap/ccloud/persephone-auditlog-receiver"
	receiverPort      = 8443
	certMountPath     = "/certs"
	cloudProviderPath = "/cloudprovider"
)

// ReceiverDeployment returns the Deployment for the auditlog receiver in the
// given namespace. image is the full image reference including tag/digest.
// container and prefix are passed as flags; prefix may use the shoot name
// already expanded by the caller.
func ReceiverDeployment(namespace, image, container, prefix string) *appsv1.Deployment {
	labels := map[string]string{
		"app":  ReceiverServiceName,
		"role": "auditlog-receiver",
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReceiverServiceName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: ptr.To(false),
					Containers: []corev1.Container{
						{
							Name:  "receiver",
							Image: image,
							Args: []string{
								"--addr=:8443",
								"--cert-dir=" + certMountPath,
								"--container=" + container,
								"--prefix=" + prefix,
							},
							Ports: []corev1.ContainerPort{
								{Name: "https", ContainerPort: receiverPort, Protocol: corev1.ProtocolTCP},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									// Cloud provider secret provides OS_AUTH_URL,
									// OS_APPLICATION_CREDENTIAL_ID, and
									// OS_APPLICATION_CREDENTIAL_SECRET.
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "cloudprovider",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "receiver-tls",
									MountPath: certMountPath,
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "receiver-tls",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: ReceiverTLSSecretName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// ReceiverService returns the ClusterIP Service for the auditlog receiver.
func ReceiverService(namespace string) *corev1.Service {
	labels := map[string]string{
		"app":  ReceiverServiceName,
		"role": "auditlog-receiver",
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ReceiverServiceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt32(receiverPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
