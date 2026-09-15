// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"

	"github.com/sap-cloud-infrastructure/persephone/internal/constants"
)

// OpenStackUserInfo contains OpenStack-specific information that can be stored in the extras of the user information.
type OpenStackUserInfo struct {
	// ProjectDomainID is the OpenStack project domain ID for the user.
	ProjectDomainID string
	// ProjectDomainName is the OpenStack project domain name for the user.
	ProjectDomainName string
	// ProjectID is the OpenStack project ID for the user.
	ProjectID string
	// ProjectName is the OpenStack project name for the user.
	ProjectName string
	// UserDomainID is the OpenStack user domain ID for the user.
	UserDomainID string
	// UserDomainName is the OpenStack user domain name for the user.
	UserDomainName string
	// Region is the OpenStack region for the user.
	Region string
}

const (
	// OpenStackProjectDomainID is a constant for the OpenStack project domain ID in the extras of the user information.
	OpenStackProjectDomainID = "project_domain_id"
	// OpenStackProjectDomainName is a constant for the OpenStack project domain name in the extras of the user information.
	OpenStackProjectDomainName = "project_domain_name"
	// OpenStackProjectID is a constant for the OpenStack project  ID in the extras of the user information.
	OpenStackProjectID = "project_id"
	// OpenStackProjectName is a constant for the OpenStack project name in the extras of the user information.
	OpenStackProjectName = "project_name"
	// OpenStackUserDomainID is a constant for the OpenStack user domain ID in the extras of the user information.
	OpenStackUserDomainID = "user_domain_id"
	// OpenStackUserDomainName is a constant for the OpenStack user domain name in the extras of the user information.
	OpenStackUserDomainName = "user_domain_name"
	// OpenStackRegion is a constant for the OpenStack region in the extras of the user information.
	OpenStackRegion = "region"
)

// ToExtra returns the map with the extra values for the user info.
func (o OpenStackUserInfo) ToExtra() map[string]authenticationv1.ExtraValue {
	return map[string]authenticationv1.ExtraValue{
		OpenStackProjectDomainID:   {o.ProjectDomainID},
		OpenStackProjectDomainName: {o.ProjectDomainName},
		OpenStackProjectID:         {o.ProjectID},
		OpenStackProjectName:       {o.ProjectName},
		OpenStackUserDomainID:      {o.UserDomainID},
		OpenStackUserDomainName:    {o.UserDomainName},
		OpenStackRegion:            {o.Region},
	}
}

// HasOpenStackUserInfo checks if the minimally needed OpenStack information is part of the user info's extras.
func HasOpenStackUserInfo(extra map[string]authenticationv1.ExtraValue) bool {
	for _, field := range []string{
		OpenStackProjectDomainID,
		OpenStackProjectDomainName,
		OpenStackProjectID,
		OpenStackProjectName,
		OpenStackUserDomainID,
		OpenStackUserDomainName,
		OpenStackRegion,
	} {
		if _, ok := extra[field]; !ok {
			return false
		}
	}

	return true
}

// OpenStackUserInfoFromExtra extracts the OpenStack specific information from the extras of the user info.
func OpenStackUserInfoFromExtra(extra map[string]authenticationv1.ExtraValue) (OpenStackUserInfo, error) {
	var (
		info = OpenStackUserInfo{}
		err  error

		getFirstValueForKey = func(key string) (string, error) {
			if values, ok := extra[key]; ok && len(values) > 0 {
				return values[0], nil
			}
			return "", fmt.Errorf("key %q not found", key)
		}
	)

	info.ProjectDomainID, err = getFirstValueForKey(OpenStackProjectDomainID)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectDomainID, err)
	}
	info.ProjectDomainName, err = getFirstValueForKey(OpenStackProjectDomainName)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectDomainName, err)
	}
	info.ProjectID, err = getFirstValueForKey(OpenStackProjectID)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectID, err)
	}
	info.ProjectName, err = getFirstValueForKey(OpenStackProjectName)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectName, err)
	}
	info.UserDomainID, err = getFirstValueForKey(OpenStackUserDomainID)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackUserDomainID, err)
	}
	info.UserDomainName, err = getFirstValueForKey(OpenStackUserDomainName)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackUserDomainName, err)
	}
	info.Region, err = getFirstValueForKey(OpenStackRegion)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackRegion, err)
	}

	return info, nil
}

// OpenStackUserInfoFromLabels extracts the OpenStack specific information from the labels.
func OpenStackUserInfoFromLabels(labels map[string]string) (OpenStackUserInfo, error) {
	var (
		info = OpenStackUserInfo{}
		err  error

		getLabelValueForKey = func(key string) (string, error) {
			if value, ok := labels[key]; ok && value != "" {
				return value, nil
			}
			return "", fmt.Errorf("key %q not found", key)
		}
	)

	info.ProjectDomainID, err = getLabelValueForKey(constants.LabelKeyOpenStackDomainID)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectDomainID, err)
	}
	info.ProjectDomainName, err = getLabelValueForKey(constants.LabelKeyOpenStackDomainName)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectDomainName, err)
	}
	info.ProjectID, err = getLabelValueForKey(constants.LabelKeyOpenStackProjectID)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectID, err)
	}
	info.ProjectName, err = getLabelValueForKey(constants.LabelKeyOpenStackProjectName)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackProjectName, err)
	}
	info.Region, err = getLabelValueForKey(constants.LabelKeyOpenStackRegion)
	if err != nil {
		return OpenStackUserInfo{}, fmt.Errorf("failed extracting %s from user info extras: %w", OpenStackRegion, err)
	}

	return info, nil
}
