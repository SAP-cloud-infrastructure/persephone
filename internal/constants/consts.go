package constants

import (
	"time"
)

const (
	// ApplicationCredentialsValidity is the default validity duration for OpenStack application credentials.
	ApplicationCredentialsValidity = 24 * 60 * time.Hour
	// ApplicationCredentialsRenewTime is the default renew time after which OpenStack credentials have to be renewed
	// (if 24h are left until expiration, renewal should be triggered).
	ApplicationCredentialsRenewTime = 24 * time.Hour

	// AnnotationKeyExpiresAt is a constant for an annotation key on a Secret whose value contains a timestamp at which
	// the credentials in the Secret's data expire.
	AnnotationKeyExpiresAt = "secret.persephone.sci.cloud.sap/expires-at"
	// AnnotationKeyForceCredentialsBindingUpdate is a constant for an annotation key on a Shoot whose value must be
	// 'true' in order to make the Shoot controller ignore the Shoot's maintenance time window and update its
	// .spec.credentialsBindingName to the newest CredentialsBinding in the system immediately.
	AnnotationKeyForceCredentialsBindingUpdate = "shoot.persephone.sci.cloud.sap/force-credentials-binding-update"

	// LabelKeyOpenStackDomainName is a constant for a label key whose value contains the OpenStack domain name.
	LabelKeyOpenStackDomainName = "sci.cloud.sap/domain-name"
	// LabelKeyOpenStackDomainID is a constant for a label key whose value contains the OpenStack domain ID.
	LabelKeyOpenStackDomainID = "sci.cloud.sap/domain-id"
	// LabelKeyOpenStackProjectName is a constant for a label key whose value contains the OpenStack project name.
	LabelKeyOpenStackProjectName = "sci.cloud.sap/project-name"
	// LabelKeyOpenStackProjectID is a constant for a label key whose value contains the OpenStack project ID.
	LabelKeyOpenStackProjectID = "sci.cloud.sap/project-id"
	// LabelKeyOpenStackRegion is a constant for a label key whose value contains the OpenStack region.
	LabelKeyOpenStackRegion = "sci.cloud.sap/region"

	// LabelKeyShootName is a constant for a label key whose value contains the name of a Shoot (usually, this label is
	// part of InternalSecrets containing application credentials).
	LabelKeyShootName = "persephone.sci.cloud.sap/shoot-name"

	// ResourceQuotaName is the name of the ResourceQuota object managed by Persephone in each project namespace.
	ResourceQuotaName = "shoot-clusters-quota"
	// ShootResourceQuotaKey is the resource key used in ResourceQuota spec/status for counting Gardener shoot clusters.
	ShootResourceQuotaKey = "count/shoots.core.gardener.cloud"

	// GardenerProviderType is a constant for the 'openstack' provider type.
	GardenerProviderType = "openstack"
)

// KeystoneAuthMethod indicates how a Keystone client is expected to authenticate with a Keystone server.
type KeystoneAuthMethod string

const (
	// KeystoneApplicationCredentialAuth is a constant for the "application-credential" Keystone authentication method.
	KeystoneApplicationCredentialAuth KeystoneAuthMethod = "application-credential-auth"
	// KeystonePasswordAuth is a constant for the "password" Keystone authentication method.
	KeystonePasswordAuth KeystoneAuthMethod = "password-auth"
)
