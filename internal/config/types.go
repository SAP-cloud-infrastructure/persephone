package config

// PersephoneConfig contains the configuration for the persephone components.
type PersephoneConfig struct {
	// ClusterUsersDomain is the domain name for cluster admin users.
	ClusterUsersDomain string `yaml:"clusterUsersDomain"`
	// Regions is a map of regions to identity endpoint and credentials.
	Regions map[string]RegionalConfig `yaml:"regions"`
	// DefaultRegion is used to fall back to if none is provided.
	DefaultRegion string `yaml:"defaultRegion"`
	// LandscapeName is initially needed for prefixing Keystone resources that would otherwise have a conflicting name
	// within the same region and project.
	LandscapeName string `yaml:"landscape"`
	// DefaultCloudProfileName is the name of the CloudProfile to use for Shoots. If empty, defaults to the provider type.
	DefaultCloudProfileName string `yaml:"defaultCloudProfileName,omitempty"`
	// DefaultProviderType is the provider type to use for Shoots. If empty, defaults to the constant GardenerProviderType.
	DefaultProviderType string `yaml:"defaultProviderType,omitempty"`
	// DefaultShootQuota is the initial quota for shoot clusters applied to a project namespace when it is first created.
	// uint64 matches the Limes/LIQUID quota type (liquid.ResourceQuotaRequest.Quota is uint64), so negative values are not possible.
	DefaultShootQuota uint64 `yaml:"defaultShootQuota"`
}

// RegionalConfig contains region-specific settings.
type RegionalConfig struct {
	// Enabled controls whether the region is enabled at all.
	Enabled bool `yaml:"enabled"`
	// IdentityEndpoint is the URL to the identity server.
	IdentityEndpoint string `yaml:"identityEndpoint"`
	// ApplicationCredentialID is the ID of the application credential.
	ApplicationCredentialID string `yaml:"applicationCredentialID"`
	// ApplicationCredentialSecret is the secret of the application credential.
	ApplicationCredentialSecret string `yaml:"applicationCredentialSecret"`
	// AuditTransportURL is the transport connection URL for audit event delivery.
	AuditTransportURL string `yaml:"auditTransportURL,omitempty"`
}
