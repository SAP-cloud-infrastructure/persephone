// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package kubernetes_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"

	. "github.com/sap-cloud-infrastructure/persephone/internal/kubernetes"
)

var _ = Describe("UserInfo", func() {
	// Hardcoded constant values to detect if constants accidentally change
	const (
		keyProjectDomainID   = "project_domain_id"
		keyProjectDomainName = "project_domain_name"
		keyProjectID         = "project_id"
		keyProjectName       = "project_name"
		keyUserDomainID      = "user_domain_id"
		keyUserDomainName    = "user_domain_name"
		keyRegion            = "region"
	)

	var (
		projectDomainID   = "project-domain-id-123"
		projectDomainName = "project-domain-name"
		projectID         = "project-id-456"
		projectName       = "project-name"
		userDomainID      = "user-domain-id-789"
		userDomainName    = "user-domain-name"
		region            = "region-1"
	)

	Describe("OpenStackUserInfo.ToExtra", func() {
		It("should convert OpenStackUserInfo to extra map with all fields", func() {
			extra := OpenStackUserInfo{
				ProjectDomainID:   projectDomainID,
				ProjectDomainName: projectDomainName,
				ProjectID:         projectID,
				ProjectName:       projectName,
				UserDomainID:      userDomainID,
				UserDomainName:    userDomainName,
				Region:            region,
			}.ToExtra()

			Expect(extra).To(HaveLen(7))
			Expect(extra[keyProjectDomainID]).To(Equal(authenticationv1.ExtraValue{projectDomainID}))
			Expect(extra[keyProjectDomainName]).To(Equal(authenticationv1.ExtraValue{projectDomainName}))
			Expect(extra[keyProjectID]).To(Equal(authenticationv1.ExtraValue{projectID}))
			Expect(extra[keyProjectName]).To(Equal(authenticationv1.ExtraValue{projectName}))
			Expect(extra[keyUserDomainID]).To(Equal(authenticationv1.ExtraValue{userDomainID}))
			Expect(extra[keyUserDomainName]).To(Equal(authenticationv1.ExtraValue{userDomainName}))
			Expect(extra[keyRegion]).To(Equal(authenticationv1.ExtraValue{region}))
		})

		It("should convert OpenStackUserInfo to extra map with empty fields", func() {
			extra := OpenStackUserInfo{}.ToExtra()

			Expect(extra).To(HaveLen(7))
			Expect(extra[keyProjectDomainID]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyProjectDomainName]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyProjectID]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyProjectName]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyUserDomainID]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyUserDomainName]).To(Equal(authenticationv1.ExtraValue{""}))
			Expect(extra[keyRegion]).To(Equal(authenticationv1.ExtraValue{""}))
		})
	})

	Describe("OpenStackUserInfoFromExtra", func() {
		It("should extract OpenStackUserInfo from valid extra map with all fields", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(info.ProjectDomainID).To(Equal(projectDomainID))
			Expect(info.ProjectDomainName).To(Equal(projectDomainName))
			Expect(info.ProjectID).To(Equal(projectID))
			Expect(info.ProjectName).To(Equal(projectName))
			Expect(info.UserDomainID).To(Equal(userDomainID))
			Expect(info.UserDomainName).To(Equal(userDomainName))
			Expect(info.Region).To(Equal(region))
		})

		It("should return error when ProjectDomainID is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectDomainName is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID: {projectDomainID},
				keyProjectID:       {projectID},
				keyProjectName:     {projectName},
				keyUserDomainID:    {userDomainID},
				keyUserDomainName:  {userDomainName},
				keyRegion:          {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectID is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectName is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when UserDomainID is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting user_domain_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when UserDomainName is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting user_domain_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when Region is missing", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {projectDomainID},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting region")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when a field has empty array", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {},
				keyProjectDomainName: {projectDomainName},
				keyProjectID:         {projectID},
				keyProjectName:       {projectName},
				keyUserDomainID:      {userDomainID},
				keyUserDomainName:    {userDomainName},
				keyRegion:            {region},
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should extract first value when multiple values are present", func() {
			var (
				firstDomainID        = "first-domain-id"
				secondDomainID       = "second-domain-id"
				firstDomainName      = "first-domain-name"
				secondDomainName     = "second-domain-name"
				firstProjectID       = "first-project-id"
				secondProjectID      = "second-project-id"
				firstProjectName     = "first-project-name"
				secondProjectName    = "second-project-name"
				firstUserDomainID    = "first-user-domain-id"
				secondUserDomainID   = "second-user-domain-id"
				firstUserDomainName  = "first-user-domain-name"
				secondUserDomainName = "second-user-domain-name"
				firstRegion          = "first-region"
				secondRegion         = "second-region"
			)

			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {firstDomainID, secondDomainID},
				keyProjectDomainName: {firstDomainName, secondDomainName},
				keyProjectID:         {firstProjectID, secondProjectID},
				keyProjectName:       {firstProjectName, secondProjectName},
				keyUserDomainID:      {firstUserDomainID, secondUserDomainID},
				keyUserDomainName:    {firstUserDomainName, secondUserDomainName},
				keyRegion:            {firstRegion, secondRegion},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(info.ProjectDomainID).To(Equal(firstDomainID))
			Expect(info.ProjectDomainName).To(Equal(firstDomainName))
			Expect(info.ProjectID).To(Equal(firstProjectID))
			Expect(info.ProjectName).To(Equal(firstProjectName))
			Expect(info.UserDomainID).To(Equal(firstUserDomainID))
			Expect(info.UserDomainName).To(Equal(firstUserDomainName))
			Expect(info.Region).To(Equal(firstRegion))
		})

		It("should handle empty string values in all fields", func() {
			info, err := OpenStackUserInfoFromExtra(map[string]authenticationv1.ExtraValue{
				keyProjectDomainID:   {""},
				keyProjectDomainName: {""},
				keyProjectID:         {""},
				keyProjectName:       {""},
				keyUserDomainID:      {""},
				keyUserDomainName:    {""},
				keyRegion:            {""},
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(info.ProjectDomainID).To(Equal(""))
			Expect(info.ProjectDomainName).To(Equal(""))
			Expect(info.ProjectID).To(Equal(""))
			Expect(info.ProjectName).To(Equal(""))
			Expect(info.UserDomainID).To(Equal(""))
			Expect(info.UserDomainName).To(Equal(""))
			Expect(info.Region).To(Equal(""))
		})
	})

	Describe("HasOpenStackUserInfo", func() {
		DescribeTable("should check if all required OpenStack fields are present",
			func(extra map[string]authenticationv1.ExtraValue, expectedResult bool) {
				Expect(HasOpenStackUserInfo(extra)).To(Equal(expectedResult))
			},

			Entry("should return true when all fields are present",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
				},
				true,
			),
			Entry("should return true when all fields are present with empty values",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {""},
					keyProjectDomainName: {""},
					keyProjectID:         {""},
					keyProjectName:       {""},
					keyUserDomainID:      {""},
					keyUserDomainName:    {""},
					keyRegion:            {""},
				},
				true,
			),
			Entry("should return true when all fields are present with additional fields",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
					"extra_field":        {"extra_value"},
				},
				true,
			),
			Entry("should return false when ProjectDomainID is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
				},
				false,
			),
			Entry("should return false when ProjectDomainName is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID: {projectDomainID},
					keyProjectID:       {projectID},
					keyProjectName:     {projectName},
					keyUserDomainID:    {userDomainID},
					keyUserDomainName:  {userDomainName},
					keyRegion:          {region},
				},
				false,
			),
			Entry("should return false when ProjectID is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
				},
				false,
			),
			Entry("should return false when ProjectName is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
				},
				false,
			),
			Entry("should return false when UserDomainID is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainName:    {userDomainName},
					keyRegion:            {region},
				},
				false,
			),
			Entry("should return false when UserDomainName is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyRegion:            {region},
				},
				false,
			),
			Entry("should return false when Region is missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID:   {projectDomainID},
					keyProjectDomainName: {projectDomainName},
					keyProjectID:         {projectID},
					keyProjectName:       {projectName},
					keyUserDomainID:      {userDomainID},
					keyUserDomainName:    {userDomainName},
				},
				false,
			),
			Entry("should return false when extra map is empty",
				map[string]authenticationv1.ExtraValue{},
				false,
			),
			Entry("should return false when extra map is nil",
				nil,
				false,
			),
			Entry("should return false when multiple fields are missing",
				map[string]authenticationv1.ExtraValue{
					keyProjectDomainID: {projectDomainID},
					keyProjectName:     {projectName},
					keyRegion:          {region},
				},
				false,
			),
		)
	})

	Describe("Round-trip conversion", func() {
		It("should successfully convert ToExtra and back for all fields", func() {
			original := OpenStackUserInfo{
				ProjectDomainID:   projectDomainID,
				ProjectDomainName: projectDomainName,
				ProjectID:         projectID,
				ProjectName:       projectName,
				UserDomainID:      userDomainID,
				UserDomainName:    userDomainName,
				Region:            region,
			}

			extra := original.ToExtra()
			extracted, err := OpenStackUserInfoFromExtra(extra)
			Expect(err).ToNot(HaveOccurred())

			Expect(extracted).To(Equal(original))
			Expect(extracted.ProjectDomainID).To(Equal(projectDomainID))
			Expect(extracted.ProjectDomainName).To(Equal(projectDomainName))
			Expect(extracted.ProjectID).To(Equal(projectID))
			Expect(extracted.ProjectName).To(Equal(projectName))
			Expect(extracted.UserDomainID).To(Equal(userDomainID))
			Expect(extracted.UserDomainName).To(Equal(userDomainName))
			Expect(extracted.Region).To(Equal(region))
		})
	})

	Describe("OpenStackUserInfoFromLabels", func() {
		const (
			labelKeyDomainID    = "sci.cloud.sap/domain-id"
			labelKeyDomainName  = "sci.cloud.sap/domain-name"
			labelKeyProjectID   = "sci.cloud.sap/project-id"
			labelKeyProjectName = "sci.cloud.sap/project-name"
			labelKeyRegion      = "sci.cloud.sap/region"
		)

		It("should extract OpenStackUserInfo from valid labels map with all fields", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(info.ProjectDomainID).To(Equal(projectDomainID))
			Expect(info.ProjectDomainName).To(Equal(projectDomainName))
			Expect(info.ProjectID).To(Equal(projectID))
			Expect(info.ProjectName).To(Equal(projectName))
			Expect(info.Region).To(Equal(region))
			// Note: UserDomainID and UserDomainName are not extracted from labels
			Expect(info.UserDomainID).To(BeEmpty())
			Expect(info.UserDomainName).To(BeEmpty())
		})

		It("should return error when ProjectDomainID is missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectDomainName is missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectID is missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectName is missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:   projectDomainID,
				labelKeyDomainName: projectDomainName,
				labelKeyProjectID:  projectID,
				labelKeyRegion:     region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when Region is missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting region")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when a field has empty string value", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    "",
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should successfully extract when additional labels are present", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
				"extra-label":       "extra-value",
				"another-label":     "another-value",
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(info.ProjectDomainID).To(Equal(projectDomainID))
			Expect(info.ProjectDomainName).To(Equal(projectDomainName))
			Expect(info.ProjectID).To(Equal(projectID))
			Expect(info.ProjectName).To(Equal(projectName))
			Expect(info.Region).To(Equal(region))
		})

		It("should return error when labels map is empty", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{})
			Expect(err).To(HaveOccurred())
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when labels map is nil", func() {
			info, err := OpenStackUserInfoFromLabels(nil)
			Expect(err).To(HaveOccurred())
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when multiple fields are missing", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:  projectDomainID,
				labelKeyProjectID: projectID,
			})
			Expect(err).To(HaveOccurred())
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectDomainName has empty string", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  "",
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_domain_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectID has empty string", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   "",
				labelKeyProjectName: projectName,
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_id")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when ProjectName has empty string", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: "",
				labelKeyRegion:      region,
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting project_name")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})

		It("should return error when Region has empty string", func() {
			info, err := OpenStackUserInfoFromLabels(map[string]string{
				labelKeyDomainID:    projectDomainID,
				labelKeyDomainName:  projectDomainName,
				labelKeyProjectID:   projectID,
				labelKeyProjectName: projectName,
				labelKeyRegion:      "",
			})
			Expect(err).To(MatchError(ContainSubstring("failed extracting region")))
			Expect(info).To(Equal(OpenStackUserInfo{}))
		})
	})
})
