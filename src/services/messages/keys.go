// Code identifiers remain stable when translations are edited.
package messages

const (
	AuthStateUnavailable ID = "authstate.unavailable"
	AuthStateInvalid     ID = "authstate.invalid"
	AuthStateDenied      ID = "authstate.denied"
	AuthStateNotFound    ID = "authstate.not_found"
	AuthStateConflict    ID = "authstate.conflict"
	AuthStateLimit       ID = "authstate.limit"

	HttpInvalidBearerToken                                                  ID = "http.invalid_bearer_token"
	SessionsInvalidTokenTTL                                                 ID = "sessions.invalid_token_ttl"
	HttpMethodNotAllowed                                                    ID = "http.method_not_allowed"
	HttpInvalidPermissionQueries                                            ID = "http.invalid_permission_queries"
	HttpInvalidPermissionQueriesActionAccess                                ID = "http.invalid_permission_queries_action_access"
	HttpInvalidPermissionQueriesActionAccessHandleActionAccess              ID = "http.invalid_permission_queries_action_access_handle_action_access"
	HttpInvalidObjectAttributes                                             ID = "http.invalid_object_attributes"
	HttpInvalidObjectAttributesObjectAttributes                             ID = "http.invalid_object_attributes_object_attributes"
	HttpMethodNotAllowedBucketUpdate                                        ID = "http.method_not_allowed_bucket_update"
	HttpInvalidFolderDelete                                                 ID = "http.invalid_folder_delete"
	HttpInvalidFolderResult                                                 ID = "http.invalid_folder_result"
	HttpInvalidMultipartID                                                  ID = "http.invalid_multipartid"
	HttpInvalidPartNumber                                                   ID = "http.invalid_part_number"
	HttpInvalidPartSize                                                     ID = "http.invalid_part_size"
	HttpInvalidDelete                                                       ID = "http.invalid_delete"
	HttpInvalidSelectQuery                                                  ID = "http.invalid_select_query"
	HttpOriginDenied                                                        ID = "http.origin_denied"
	HttpInvalidTierOperation                                                ID = "http.invalid_tier_operation"
	HttpInvalidUploadSize                                                   ID = "http.invalid_upload_size"
	HttpOriginDeniedUploadActions                                           ID = "http.origin_denied_upload_actions"
	HttpInvalidUpload                                                       ID = "http.invalid_upload"
	InternalError                                                           ID = "http.internal_error"
	HttpSessionExpired                                                      ID = "http.session_expired"
	HttpSessionExpiredSession                                               ID = "http.session_expired_session"
	HttpCSRFDenied                                                          ID = "http.csrf_denied"
	HttpConfirmationRequired                                                ID = "http.confirmation_required"
	HttpJSONRequired                                                        ID = "http.json_required"
	HttpInvalidJSON                                                         ID = "http.invalidjson"
	HttpInvalidJSONResponse                                                 ID = "http.invalidjson_response"
	AccessAccessDenied                                                      ID = "access.access_denied"
	ApperrorCouldNotVerifyTheCredentialsCheckThe                            ID = "apperror.could_not_verify_the_credentials_check_the"
	ApperrorInsufficientMinIOPermissionsForThisOperation                    ID = "apperror.insufficient_minio_permissions_for_this_operation"
	ApperrorTheResourceNoLongerExistsRefreshThe                             ID = "apperror.the_resource_no_longer_exists_refresh_the"
	ApperrorTheBucketStateDoesNotAllowThis                                  ID = "apperror.the_bucket_state_does_not_allow_this"
	ApperrorTheBucketIsNotEmptyThisOperation                                ID = "apperror.the_bucket_is_not_empty_this_operation"
	ApperrorTheStorageRejectedTheDataBecauseThe                             ID = "apperror.the_storage_rejected_the_data_because_the"
	ApperrorTheStorageRejectedTheChecksumFormatCheck                        ID = "apperror.the_storage_rejected_the_checksum_format_check"
	ApperrorTheResourceAlreadyExistsOrIsIn                                  ID = "apperror.the_resource_already_exists_or_is_in"
	ApperrorMinIORejectedTheParametersCheckTheName                          ID = "apperror.minio_rejected_the_parameters_check_the_name"
	ApperrorTheMinIOTokenIsInvalidOrExpired                                 ID = "apperror.the_minio_token_is_invalid_or_expired"
	ApperrorThisServerVersionOrTypeDoesNot                                  ID = "apperror.this_server_version_or_type_does_not"
	ApperrorTheS3APIDidNotRespondIn                                         ID = "apperror.thes3api_did_not_respond_in"
	ApperrorCouldNotCompleteTheMinIORequestCheck                            ID = "apperror.could_not_complete_the_minio_request_check"
	ArchivesArchiveExpired                                                  ID = "archives.archive_expired"
	ArchivesArchiveStarted                                                  ID = "archives.archive_started"
	ArchivesTheFileChangedAfterVerificationPrepareA                         ID = "archives.the_file_changed_after_verification_preparea"
	ArchivesCouldNotReadTheEntireFileDo                                     ID = "archives.could_not_read_the_entire_file_do"
	ArchivesInvalidGzip                                                     ID = "archives.invalid_gzip"
	ArchivesInvalidGzipGzipView                                             ID = "archives.invalid_gzip_gzip_view"
	ArchivesGzipTextTooLarge                                                ID = "archives.gzip_text_too_large"
	ArchivesGzipTooManyLines                                                ID = "archives.gzip_too_many_lines"
	ArchivesNotUTF8Text                                                     ID = "archives.notutf8_text"
	ArchivesNotGzip                                                         ID = "archives.not_gzip"
	ArchivesGzipTooLarge                                                    ID = "archives.gzip_too_large"
	ArchivesObjectChanged                                                   ID = "archives.object_changed"
	ArchivesArchiveStartedModel                                             ID = "archives.archive_started_model"
	ArchivesInvalidArchiveBase                                              ID = "archives.invalid_archive_base"
	ArchivesArchiveRunning                                                  ID = "archives.archive_running"
	ArchivesTheTransferWasNotStarted                                        ID = "archives.the_transfer_was_not_started"
	ArchivesEmptyArchive                                                    ID = "archives.empty_archive"
	ArchivesArchivePartialConsent                                           ID = "archives.archive_partial_consent"
	ArchivesZIPCompletionWasNotConfirmedPrepareA                            ID = "archives.zip_completion_was_not_confirmed_preparea"
	ArchivesTheTransferWasStoppedTheZIPMay                                  ID = "archives.the_transfer_was_stopped_thezip_may"
	ArchivesInvalidArchiveName                                              ID = "archives.invalid_archive_name"
	ArchivesInvalidArchiveBasePaths                                         ID = "archives.invalid_archive_base_paths"
	ArchivesInvalidArchiveBasePathsArchivePaths                             ID = "archives.invalid_archive_base_paths_archive_paths"
	ArchivesArchivePathConflict                                             ID = "archives.archive_path_conflict"
	ArchivesInvalidArchive                                                  ID = "archives.invalid_archive"
	ArchivesInvalidArchivePrepareUsecase                                    ID = "archives.invalid_archive_prepare_usecase"
	ArchivesInvalidArchivePrepareUsecasePrepareArchive                      ID = "archives.invalid_archive_prepare_usecase_prepare_archive"
	ArchivesInvalidArchivePrepareUsecasePrepareArchiveD520E6                ID = "archives.invalid_archive_prepare_usecase_prepare_archived520e6"
	ArchivesInvalidArchivePrepareUsecasePrepareArchive4AEA25                ID = "archives.invalid_archive_prepare_usecase_prepare_archive4aea25"
	ArchivesInvalidArchivePrepareUsecasePrepareArchive0BA8AF                ID = "archives.invalid_archive_prepare_usecase_prepare_archive0ba8af"
	ArchivesInvalidArchivePrepareUsecasePrepareArchive3EDEA0                ID = "archives.invalid_archive_prepare_usecase_prepare_archive3edea0"
	ArchivesArchiveTooLarge                                                 ID = "archives.archive_too_large"
	ArchivesUnsafeArchivePath                                               ID = "archives.unsafe_archive_path"
	ArchivesThisPathCannotBeSafelyStoredIn                                  ID = "archives.this_path_cannot_be_safely_stored_in"
	ArchivesArchiveTooLargePrepareUsecase                                   ID = "archives.archive_too_large_prepare_usecase"
	ArchivesEmptyArchivePrepareUsecase                                      ID = "archives.empty_archive_prepare_usecase"
	ArchivesUnsafePath                                                      ID = "archives.unsafe_path"
	ArchivesSymbolicLink                                                    ID = "archives.symbolic_link"
	ArchivesTheArchiveIsEncrypted                                           ID = "archives.the_archive_is_encrypted"
	ArchivesTheCompressionMethodIsNotSupported                              ID = "archives.the_compression_method_is_not_supported"
	ArchivesTheFileExceeds256MiB                                            ID = "archives.the_file_exceeds256_mib"
	ArchivesInvalidDestination                                              ID = "archives.invalid_destination"
	ArchivesZIPChanged                                                      ID = "archives.zip_changed"
	ArchivesZIPDirectory                                                    ID = "archives.zip_directory"
	ArchivesCorruptZIPEntry                                                 ID = "archives.corruptzip_entry"
	ArchivesCorruptZIPEntryZipExplorerUsecase                               ID = "archives.corruptzip_entry_zip_explorer_usecase"
	ArchivesZIPClosed                                                       ID = "archives.zip_closed"
	ArchivesInvalidZIPEntry                                                 ID = "archives.invalidzip_entry"
	ArchivesInvalidZIP                                                      ID = "archives.invalidzip"
	ArchivesZIPLimit                                                        ID = "archives.zip_limit"
	ArchivesInvalidZIPReader                                                ID = "archives.invalidzip_reader"
	ArchivesZIPLimitReader                                                  ID = "archives.zip_limit_reader"
	ArchivesInvalidZIPReaderCheckedZIP                                      ID = "archives.invalidzip_reader_checkedzip"
	ArchivesInvalidZIPReaderCheckedZIPD112EC                                ID = "archives.invalidzip_reader_checkedzipd112ec"
	ArchivesInvalidZIPReaderCheckedZIP9AD98F                                ID = "archives.invalidzip_reader_checkedzip9ad98f"
	ArchivesZIPLimitReaderLoadStoredZIP                                     ID = "archives.zip_limit_reader_load_storedzip"
	ArchivesZIPChangedReader                                                ID = "archives.zip_changed_reader"
	BatchInvalidBatch                                                       ID = "batch.invalid_batch"
	BatchConfirmationRequired                                               ID = "batch.confirmation_required"
	BatchInvalidBatchParameters                                             ID = "batch.invalid_batch_parameters"
	BucketsCouldNotReadPrincipal                                            ID = "buckets.could_not_read_principal"
	BucketsInvalidPolicyDefinition                                          ID = "buckets.invalid_policy_definition"
	BucketsInvalidBucketName                                                ID = "buckets.invalid_bucket_name"
	BucketsCouldNotParseOneOfTheIAM                                         ID = "buckets.could_not_parse_one_of_theiam"
	BucketsShowingAssignmentsForTheFirst1000                                ID = "buckets.showing_assignments_for_the_first1000"
	BucketsIAMPolicyAssignmentsWereOnlyPartiallyRead                        ID = "buckets.iam_policy_assignments_were_only_partially_read"
	BucketsUsersWereOnlyPartiallyRead                                       ID = "buckets.users_were_only_partially_read"
	BucketsGroupsWereOnlyPartiallyRead                                      ID = "buckets.groups_were_only_partially_read"
	BucketsCheckedTheFirst500Groups                                         ID = "buckets.checked_the_first500_groups"
	BucketsTheGroupCheckTimedOut                                            ID = "buckets.the_group_check_timed_out"
	BucketsCouldNotRetrieveMembersAndStatusFor                              ID = "buckets.could_not_retrieve_members_and_status_for"
	BucketsDirect                                                           ID = "buckets.direct"
	BucketsGroup                                                            ID = "buckets.group"
	Buckets                                                                 ID = "buckets."
	BucketsMemberOfDisabledGroups                                           ID = "buckets.member_of_disabled_groups"
	BucketsApplicationKeysAreUnavailable                                    ID = "buckets.application_keys_are_unavailable"
	BucketsOwner                                                            ID = "buckets.owner"
	BucketsTheKeyRestrictionsCouldNotBeRead                                 ID = "buckets.the_key_restrictions_could_not_be_read"
	BucketsCouldNotReadAllKeyRestrictions                                   ID = "buckets.could_not_read_all_key_restrictions"
	BucketsTheKeyOwnerChangedRefreshTheDetails                              ID = "buckets.the_key_owner_changed_refresh_the_details"
	BucketsAKeyOwnerChangedDuringTheCheck                                   ID = "buckets.a_key_owner_changed_during_the_check"
	BucketsCouldNotReadTheRestrictionPolicyFor                              ID = "buckets.could_not_read_the_restriction_policy_for"
	BucketsTheOwnerBelongsToDisabledGroups                                  ID = "buckets.the_owner_belongs_to_disabled_groups"
	BucketsTheOwnerUserIsDisabled                                           ID = "buckets.the_owner_user_is_disabled"
	BucketsTheOwnerSPermissionsAreNotListed                                 ID = "buckets.the_owners_permissions_are_not_listed"
	BucketsCheckedTheFirst500ApplicationKeys                                ID = "buckets.checked_the_first500_application_keys"
	BucketsTheBucketPolicyCouldNotBeRead                                    ID = "buckets.the_bucket_policy_could_not_be_read"
	BucketsTheBucketPolicyHasAnUnsupportedFormat                            ID = "buckets.the_bucket_policy_has_an_unsupported_format"
	BucketsBucketPolicy                                                     ID = "buckets.bucket_policy"
	BucketsPrincipalIsEvaluatedByMinIO                                      ID = "buckets.principal_is_evaluated_by_minio"
	BucketsTheseRulesMayAllowAnonymousAccessOr                              ID = "buckets.these_rules_may_allow_anonymous_access_or"
	BucketsThisSettingContainsParametersThatTheCurrent                      ID = "buckets.this_setting_contains_parameters_that_the_current"
	BucketsInvalidCORS                                                      ID = "buckets.invalidcors"
	BucketsBucketSettingChanged                                             ID = "buckets.bucket_setting_changed"
	BucketsCORSRuleNotFound                                                 ID = "buckets.cors_rule_not_found"
	BucketsInvalidCORSBucketBrowserSettingsChange                           ID = "buckets.invalidcors_bucket_browser_settings_change"
	BucketsInvalidCORSBucketBrowserSettingsChangeChangeBrowserSetting       ID = "buckets.invalidcors_bucket_browser_settings_change_change_browser_setting"
	BucketsCORSDuplicateID                                                  ID = "buckets.cors_duplicateid"
	BucketsConfirmationRequired                                             ID = "buckets.confirmation_required"
	BucketsInvalidEncryption                                                ID = "buckets.invalid_encryption"
	BucketsConfirmationRequiredBucketBrowserSettingsChange                  ID = "buckets.confirmation_required_bucket_browser_settings_change"
	BucketsInvalidEncryptionBucketBrowserSettingsChange                     ID = "buckets.invalid_encryption_bucket_browser_settings_change"
	BucketsInvalidEncryptionBucketBrowserSettingsChangeChangeBrowserSetting ID = "buckets.invalid_encryption_bucket_browser_settings_change_change_browser_setting"
	BucketsKMSUnavailable                                                   ID = "buckets.kms_unavailable"
	BucketsInvalidBucketSetting                                             ID = "buckets.invalid_bucket_setting"
	BucketsBucketSettingWriteFailed                                         ID = "buckets.bucket_setting_write_failed"
	BucketsObjectTag                                                        ID = "buckets.object_tag"
	BucketsSizeGreaterThan                                                  ID = "buckets.size_greater_than"
	BucketsSizeLessThan                                                     ID = "buckets.size_less_than"
	BucketsAbortIncompleteUploadsAfter                                      ID = "buckets.abort_incomplete_uploads_after"
	BucketsDeleteAllVersionsWhenTheCurrentVersion                           ID = "buckets.delete_all_versions_when_the_current_version"
	BucketsDeleteAllVersionsAfter                                           ID = "buckets.delete_all_versions_after"
	BucketsDeleteTheMarkerAndItsVersionsAfter                               ID = "buckets.delete_the_marker_and_its_versions_after"
	BucketsCompressTheCurrentVersion                                        ID = "buckets.compress_the_current_version"
	BucketsCompressOlderVersions                                            ID = "buckets.compress_older_versions"
	BucketsTheConfigurationContainsParametersThatTheCurrent                 ID = "buckets.the_configuration_contains_parameters_that_the_current"
	BucketsTheRuleNameMustContain1To                                        ID = "buckets.the_rule_name_must_contain1_to"
	BucketsThePathMustBeAtMost1                                             ID = "buckets.the_path_must_be_at_most1"
	BucketsPeriodsAndRetainedVersionCountsMustBe                            ID = "buckets.periods_and_retained_version_counts_must_be"
	BucketsChooseAnAgeInDaysADate                                           ID = "buckets.choose_an_age_in_daysa_date"
	BucketsSpecifyAValidTransitionAgeInDays                                 ID = "buckets.specifya_valid_transition_age_in_days"
	BucketsATierNameMustContain3To                                          ID = "buckets.a_tier_name_must_contain3_to"
	BucketsSpecifyATransitionDateOrAnAge                                    ID = "buckets.specifya_transition_date_or_an_age"
	BucketsSelectATierForCurrentVersions                                    ID = "buckets.selecta_tier_for_current_versions"
	BucketsTheTransitionAgeForOlderVersionsMust                             ID = "buckets.the_transition_age_for_older_versions_must"
	BucketsSelectATierForOlderVersions                                      ID = "buckets.selecta_tier_for_older_versions"
	BucketsSpecifyAValidExpirationDateInYYYY                                ID = "buckets.specifya_valid_expiration_date_inyyyy"
	BucketsAddAtLeastOneActionDeletingVersions                              ID = "buckets.add_at_least_one_action_deleting_versions"
	BucketsBucketSettingChangedBucketLifecycleChange                        ID = "buckets.bucket_setting_changed_bucket_lifecycle_change"
	BucketsLifecycleRuleNotFound                                            ID = "buckets.lifecycle_rule_not_found"
	BucketsInvalidLifecycleRule                                             ID = "buckets.invalid_lifecycle_rule"
	BucketsLifecycleLimit                                                   ID = "buckets.lifecycle_limit"
	BucketsInvalidLifecycleRuleBucketLifecycleChange                        ID = "buckets.invalid_lifecycle_rule_bucket_lifecycle_change"
	BucketsConfirmationRequiredBucketLifecycleChange                        ID = "buckets.confirmation_required_bucket_lifecycle_change"
	BucketsMethodNotAllowed                                                 ID = "buckets.method_not_allowed"
	BucketsInvalidLifecycleRuleBucketLifecycleChangeChangeLifecycle         ID = "buckets.invalid_lifecycle_rule_bucket_lifecycle_change_change_lifecycle"
	BucketsAccessDenied                                                     ID = "buckets.access_denied"
	BucketsLifecycleWriteFailed                                             ID = "buckets.lifecycle_write_failed"
	BucketsInvalidRetention                                                 ID = "buckets.invalid_retention"
	BucketsInvalidRetentionBucketRetention                                  ID = "buckets.invalid_retention_bucket_retention"
	BucketsInvalidRetentionBucketRetentionParseRetention                    ID = "buckets.invalid_retention_bucket_retention_parse_retention"
	BucketsBucketSettingChangedBucketUpdate                                 ID = "buckets.bucket_setting_changed_bucket_update"
	BucketsInvalidTags                                                      ID = "buckets.invalid_tags"
	BucketsInvalidTagsBucketUpdate                                          ID = "buckets.invalid_tags_bucket_update"
	BucketsInvalidTagsBucketUpdateValidateBucketTags                        ID = "buckets.invalid_tags_bucket_update_validate_bucket_tags"
	BucketsInvalidTagsBucketUpdateValidateBucketTagsB75523                  ID = "buckets.invalid_tags_bucket_update_validate_bucket_tagsb75523"
	BucketsInvalidBucketNameCatalogChange                                   ID = "buckets.invalid_bucket_name_catalog_change"
	BucketsAlreadyExists                                                    ID = "buckets.already_exists"
	BucketsInvalidBucketField                                               ID = "buckets.invalid_bucket_field"
	BucketsInvalidVersioning                                                ID = "buckets.invalid_versioning"
	BucketsInvalidObjectLock                                                ID = "buckets.invalid_object_lock"
	BucketsVersioningRestricted                                             ID = "buckets.versioning_restricted"
	BucketsVersioningRequired                                               ID = "buckets.versioning_required"
	BucketsObjectLockUnavailable                                            ID = "buckets.object_lock_unavailable"
	BucketsInvalidPolicy                                                    ID = "buckets.invalid_policy"
	BucketsInvalidPolicySettingsChange                                      ID = "buckets.invalid_policy_settings_change"
	BucketsInvalidQuota                                                     ID = "buckets.invalid_quota"
	CapabilitiesInvalidCapability                                           ID = "capabilities.invalid_capability"
	CapabilitiesInvalidCapabilityScope                                      ID = "capabilities.invalid_capability_scope"
	CapabilitiesInvalidCapabilityScopeReportUsecase                         ID = "capabilities.invalid_capability_scope_report_usecase"
	EndpointInvalidEndpoint                                                 ID = "endpoint.invalid_endpoint"
	EndpointInvalidEndpointEndpoint                                         ID = "endpoint.invalid_endpoint_endpoint"
	EndpointEndpointNotAllowed                                              ID = "endpoint.endpoint_not_allowed"
	EndpointEndpointUnavailable                                             ID = "endpoint.endpoint_unavailable"
	EndpointEndpointNotAllowedEndpoint                                      ID = "endpoint.endpoint_not_allowed_endpoint"
	FilesInvalidContentSearch                                               ID = "files.invalid_content_search"
	FilesInvalidContentExtensions                                           ID = "files.invalid_content_extensions"
	FilesInvalidUsageScope                                                  ID = "files.invalid_usage_scope"
	FilesInvalidUsageCursor                                                 ID = "files.invalid_usage_cursor"
	FilesUsageVersionsUnavailable                                           ID = "files.usage_versions_unavailable"
	FilesInvalidUsagePage                                                   ID = "files.invalid_usage_page"
	FilesInvalidUsagePageObjectUsageUsecase                                 ID = "files.invalid_usage_page_object_usage_usecase"
	FilesInvalidUsagePageObjectUsageUsecaseReadObjectUsage                  ID = "files.invalid_usage_page_object_usage_usecase_read_object_usage"
	FilesInvalidVersion                                                     ID = "files.invalid_version"
	FilesInvalidProtection                                                  ID = "files.invalid_protection"
	FilesConfirmationRequired                                               ID = "files.confirmation_required"
	FilesInvalidHold                                                        ID = "files.invalid_hold"
	FilesInvalidRetention                                                   ID = "files.invalid_retention"
	FilesObjectProtected                                                    ID = "files.object_protected"
	FilesInvalidListing                                                     ID = "files.invalid_listing"
	FilesUnsupportedPreview                                                 ID = "files.unsupported_preview"
	FilesPreviewTooLarge                                                    ID = "files.preview_too_large"
	FilesObjectChanged                                                      ID = "files.object_changed"
	FilesObjectChangedObjectsUsecase                                        ID = "files.object_changed_objects_usecase"
	FilesUnsupportedMedia                                                   ID = "files.unsupported_media"
	FilesInvalidLifetime                                                    ID = "files.invalid_lifetime"
	FilesInvalidMode                                                        ID = "files.invalid_mode"
	FoldersInvalidFolderPath                                                ID = "folders.invalid_folder_path"
	FoldersInvalidFolderListing                                             ID = "folders.invalid_folder_listing"
	FoldersInvalidFolderListingFolderDelete                                 ID = "folders.invalid_folder_listing_folder_delete"
	FoldersFolderDeleteLimit                                                ID = "folders.folder_delete_limit"
	FoldersInvalidFolderDelete                                              ID = "folders.invalid_folder_delete"
	FoldersConfirmationRequired                                             ID = "folders.confirmation_required"
	FoldersRevisionRequired                                                 ID = "folders.revision_required"
	FoldersObjectChanged                                                    ID = "folders.object_changed"
	FoldersFolderNotEmpty                                                   ID = "folders.folder_not_empty"
	FoldersObjectDeleteDenied                                               ID = "folders.object_delete_denied"
	FoldersInvalidRange                                                     ID = "folders.invalid_range"
	FoldersInvalidFolderName                                                ID = "folders.invalid_folder_name"
	FoldersInvalidFolderPathFolders                                         ID = "folders.invalid_folder_path_folders"
	FoldersInvalidFolderPathFoldersFolderKey                                ID = "folders.invalid_folder_path_folders_folder_key"
	FoldersInvalidFolderNameFolders                                         ID = "folders.invalid_folder_name_folders"
	FoldersFolderExists                                                     ID = "folders.folder_exists"
	FoldersFolderShareExpired                                               ID = "folders.folder_share_expired"
	FoldersFolderScopeDenied                                                ID = "folders.folder_scope_denied"
	FoldersFolderScopeDeniedPublicUsecase                                   ID = "folders.folder_scope_denied_public_usecase"
	FoldersInvalidTicket                                                    ID = "folders.invalid_ticket"
	FoldersUnsupportedPreview                                               ID = "folders.unsupported_preview"
	FoldersUnsupportedPreviewPublicUsecase                                  ID = "folders.unsupported_preview_public_usecase"
	FoldersPreviewTooLarge                                                  ID = "folders.preview_too_large"
	FoldersInvalidText                                                      ID = "folders.invalid_text"
	FoldersNotFound                                                         ID = "folders.not_found"
	FoldersDownloadExpired                                                  ID = "folders.download_expired"
	FoldersFolderScopeDeniedPublicUsecaseOpenSharedFile                     ID = "folders.folder_scope_denied_public_usecase_open_shared_file"
	FoldersFolderShareExpiredModel                                          ID = "folders.folder_share_expired_model"
	FoldersSharedReadClosed                                                 ID = "folders.shared_read_closed"
	FoldersInvalidFolderShare                                               ID = "folders.invalid_folder_share"
	FoldersSharePermissionsUnavailable                                      ID = "folders.share_permissions_unavailable"
	FoldersFolderShareDenied                                                ID = "folders.folder_share_denied"
	IamMinIODidNotReturnActionPermissionsIn                                 ID = "iam.minio_did_not_return_action_permissions_in"
	IamMinIODeniedReadingActionPermissionsModificationButtons               ID = "iam.minio_denied_reading_action_permissions_modification_buttons"
	IamTheServerDoesNotSupportCheckingAction                                ID = "iam.the_server_does_not_support_checking_action"
	IamCouldNotRetrieveActionPermissionsFromMinIO                           ID = "iam.could_not_retrieve_action_permissions_from_minio"
	IamMinIODidNotReturnTheConnectionOwner                                  ID = "iam.minio_did_not_return_the_connection_owner"
	IamCouldNotReadTheIAMPolicyFormat                                       ID = "iam.could_not_read_theiam_policy_format"
	IamCouldNotConfirmTheKeyOwnerModification                               ID = "iam.could_not_confirm_the_key_owner_modification"
	IamMinIOReportsThatTheKeyIsDisabled                                     ID = "iam.minio_reports_that_the_key_is_disabled"
	IamCouldNotReadTheKeyRestrictionsModification                           ID = "iam.could_not_read_the_key_restrictions_modification"
	IamCouldNotReadMinIOSConnectionOwner                                    ID = "iam.could_not_read_minios_connection_owner"
	IamTheMinIOPermissionsResponseIsTooLarge                                ID = "iam.the_minio_permissions_response_is_too_large"
	IamTheMinIOAddressOrProxyDoesNot                                        ID = "iam.the_minio_address_or_proxy_does_not"
	IamMinIOOrItsProxyReturnedAnError                                       ID = "iam.minio_or_its_proxy_returned_an_error"
	IdentitiesInvalidTarget                                                 ID = "identities.invalid_target"
	IdentitiesInvalidTargetBindings                                         ID = "identities.invalid_target_bindings"
	IdentitiesInvalidBinding                                                ID = "identities.invalid_binding"
	IdentitiesInvalidGroup                                                  ID = "identities.invalid_group"
	IdentitiesAlreadyExists                                                 ID = "identities.already_exists"
	IdentitiesGroupCreateAcknowledgementRequired                            ID = "identities.group_create_acknowledgement_required"
	IdentitiesInvalidStatus                                                 ID = "identities.invalid_status"
	IdentitiesGroupNotEmpty                                                 ID = "identities.group_not_empty"
	IdentitiesTheDetailsWereNotRetrievedWithinThe                           ID = "identities.the_details_were_not_retrieved_within_the"
	IdentitiesInvalidPolicyDefinition                                       ID = "identities.invalid_policy_definition"
	IdentitiesInvalidPolicyName                                             ID = "identities.invalid_policy_name"
	IdentitiesAlreadyExistsPolicies                                         ID = "identities.already_exists_policies"
	IdentitiesPolicyCreateAcknowledgementRequired                           ID = "identities.policy_create_acknowledgement_required"
	IdentitiesPolicyChanged                                                 ID = "identities.policy_changed"
	IdentitiesPolicyInUse                                                   ID = "identities.policy_in_use"
	IdentitiesTheAssignmentsWereNotRetrievedWithinThe                       ID = "identities.the_assignments_were_not_retrieved_within_the"
	IdentitiesTheDatesWereNotRetrievedWithinThe                             ID = "identities.the_dates_were_not_retrieved_within_the"
	IdentitiesInvalidUser                                                   ID = "identities.invalid_user"
	IdentitiesAlreadyExistsUsers                                            ID = "identities.already_exists_users"
	IdentitiesUserCreateAcknowledgementRequired                             ID = "identities.user_create_acknowledgement_required"
	IdentitiesCurrentUser                                                   ID = "identities.current_user"
	IdentitiesCurrentUserUsers                                              ID = "identities.current_user_users"
	KeysAccessKeyNotFound                                                   ID = "keys.access_key_not_found"
	KeysUnreadableAccessKeyPolicy                                           ID = "keys.unreadable_access_key_policy"
	KeysInvalidKeyName                                                      ID = "keys.invalid_key_name"
	KeysInvalidKeyDescription                                               ID = "keys.invalid_key_description"
	KeysInvalidParent                                                       ID = "keys.invalid_parent"
	KeysInvalidPolicy                                                       ID = "keys.invalid_policy"
	KeysInvalidPolicyAccessKeys                                             ID = "keys.invalid_policy_access_keys"
	KeysInvalidPolicyAccessKeysValidateKeyInput                             ID = "keys.invalid_policy_access_keys_validate_key_input"
	KeysInvalidExpiration                                                   ID = "keys.invalid_expiration"
	KeysAccessKeyChanged                                                    ID = "keys.access_key_changed"
	KeysImmutableParent                                                     ID = "keys.immutable_parent"
	KeysEmptyKeyMetadata                                                    ID = "keys.empty_key_metadata"
	KeysInvalidScope                                                        ID = "keys.invalid_scope"
	KeysCurrentAccessKey                                                    ID = "keys.current_access_key"
	KeysConfirmationRequired                                                ID = "keys.confirmation_required"
	ObjectsChecksumStarted                                                  ID = "objects.checksum_started"
	ObjectsChecksumClosed                                                   ID = "objects.checksum_closed"
	ObjectsChecksumNotPrepared                                              ID = "objects.checksum_not_prepared"
	ObjectsInvalidObject                                                    ID = "objects.invalid_object"
	ObjectsInvalidExpectedChecksum                                          ID = "objects.invalid_expected_checksum"
	ObjectsTheStorageDidNotReturnChecksumsYou                               ID = "objects.the_storage_did_not_return_checksums_you"
	ObjectsIncompleteChecksumRead                                           ID = "objects.incomplete_checksum_read"
	ObjectsObjectChanged                                                    ID = "objects.object_changed"
	ObjectsInvalidVersion                                                   ID = "objects.invalid_version"
	ObjectsInvalidObjectAttributes                                          ID = "objects.invalid_object_attributes"
	ObjectsInvalidAttributesSnapshot                                        ID = "objects.invalid_attributes_snapshot"
	ObjectsAttributesSnapshotRequired                                       ID = "objects.attributes_snapshot_required"
	ObjectsObjectAttributesChanged                                          ID = "objects.object_attributes_changed"
	ObjectsAttributesSnapshotUnavailable                                    ID = "objects.attributes_snapshot_unavailable"
	ObjectsNotEditable                                                      ID = "objects.not_editable"
	ObjectsTextTooLarge                                                     ID = "objects.text_too_large"
	ObjectsEncodedText                                                      ID = "objects.encoded_text"
	ObjectsObjectChangedObjectContentUsecase                                ID = "objects.object_changed_object_content_usecase"
	ObjectsInvalidText                                                      ID = "objects.invalid_text"
	ObjectsInvalidTextObjectContentUsecase                                  ID = "objects.invalid_text_object_content_usecase"
	ObjectsInvalidDocument                                                  ID = "objects.invalid_document"
	ObjectsContentChanged                                                   ID = "objects.content_changed"
	ObjectsContentProtected                                                 ID = "objects.content_protected"
	ObjectsConfirmationRequired                                             ID = "objects.confirmation_required"
	ObjectsRevisionRequired                                                 ID = "objects.revision_required"
	ObjectsObjectChangedObjectDeleteUsecase                                 ID = "objects.object_changed_object_delete_usecase"
	ObjectsObjectDeleteDenied                                               ID = "objects.object_delete_denied"
	ObjectsTagsAreUnavailableCheckPermissionToRead                          ID = "objects.tags_are_unavailable_check_permission_to_read"
	ObjectsEditingMetadataForThisEncryptionTypeIs                           ID = "objects.editing_metadata_for_this_encryption_type_is"
	ObjectsMetadataOfAProtectedObjectIsRead                                 ID = "objects.metadata_ofa_protected_object_is_read"
	ObjectsInvalidPropertyMode                                              ID = "objects.invalid_property_mode"
	ObjectsInvalidObjectHeaders                                             ID = "objects.invalid_object_headers"
	ObjectsInvalidObjectHeadersObjectProperties                             ID = "objects.invalid_object_headers_object_properties"
	ObjectsInvalidObjectMetadata                                            ID = "objects.invalid_object_metadata"
	ObjectsInvalidObjectMetadataObjectProperties                            ID = "objects.invalid_object_metadata_object_properties"
	ObjectsInvalidObjectMetadataObjectPropertiesValidatePropertyPatch       ID = "objects.invalid_object_metadata_object_properties_validate_property_patch"
	ObjectsInvalidObjectTags                                                ID = "objects.invalid_object_tags"
	ObjectsInvalidObjectObjectPropertiesUsecase                             ID = "objects.invalid_object_object_properties_usecase"
	ObjectsObjectPropertiesChanged                                          ID = "objects.object_properties_changed"
	ObjectsEmptyPropertyPatch                                               ID = "objects.empty_property_patch"
	ObjectsObjectPropertiesChangedObjectPropertyTarget                      ID = "objects.object_properties_changed_object_property_target"
	ObjectsObjectChangedObjectPropertyTarget                                ID = "objects.object_changed_object_property_target"
	ObjectsObjectPropertiesChangedObjectPropertyTargetTaggedTarget          ID = "objects.object_properties_changed_object_property_target_tagged_target"
	ObjectsObjectTagsUnverified                                             ID = "objects.object_tags_unverified"
	ObjectsCopyUnsupported                                                  ID = "objects.copy_unsupported"
	ObjectsInvalidCursor                                                    ID = "objects.invalid_cursor"
	ObjectsInvalidCursorObjectVersions                                      ID = "objects.invalid_cursor_object_versions"
	ObjectsVersionListingFailed                                             ID = "objects.version_listing_failed"
	ObjectsVersionListingFailedObjectVersions                               ID = "objects.version_listing_failed_object_versions"
	ObjectsInvalidObjectObjects                                             ID = "objects.invalid_object_objects"
	ObjectsDestinationChanged                                               ID = "objects.destination_changed"
	ObjectsDestinationChangedConditional                                    ID = "objects.destination_changed_conditional"
	RemotesourceSourceResponse                                              ID = "remotesource.source_response"
	RemotesourceSourceUnavailable                                           ID = "remotesource.source_unavailable"
	RemotesourceSourceDenied                                                ID = "remotesource.source_denied"
	RemotesourceSourceDNS                                                   ID = "remotesource.sourcedns"
	RemotesourceSourceDeniedClient                                          ID = "remotesource.source_denied_client"
	RemotesourceTooManyRedirects                                            ID = "remotesource.too_many_redirects"
	RuntimeAccountInfoTooLarge                                              ID = "runtime.account_info_too_large"
	RuntimeTransferLimit                                                    ID = "runtime.transfer_limit"
	RuntimeLoginRateLimit                                                   ID = "runtime.login_rate_limit"
	RuntimeTooManyRequests                                                  ID = "runtime.too_many_requests"
	RuntimeLoginRateLimitAdmission                                          ID = "runtime.login_rate_limit_admission"
	RuntimeArchiveLimit                                                     ID = "runtime.archive_limit"
	RuntimeCapabilityBusy                                                   ID = "runtime.capability_busy"
	RuntimeInviteLimit                                                      ID = "runtime.invite_limit"
	RuntimeInviteExists                                                     ID = "runtime.invite_exists"
	RuntimeInviteNotFound                                                   ID = "runtime.invite_not_found"
	RuntimeMultipartLimit                                                   ID = "runtime.multipart_limit"
	RuntimeMultipartExists                                                  ID = "runtime.multipart_exists"
	RuntimeServerClosing                                                    ID = "runtime.server_closing"
	RuntimeSessionExpired                                                   ID = "runtime.session_expired"
	RuntimeSessionLimit                                                     ID = "runtime.session_limit"
	RuntimeShareLimit                                                       ID = "runtime.share_limit"
	RuntimeTicketLimit                                                      ID = "runtime.ticket_limit"
	RuntimeZIPBusy                                                          ID = "runtime.zip_busy"
	SearchEncodedText                                                       ID = "search.encoded_text"
	SearchSearchFileTooLarge                                                ID = "search.search_file_too_large"
	SearchSearchObjectChanged                                               ID = "search.search_object_changed"
	SearchInvalidObjectListing                                              ID = "search.invalid_object_listing"
	SearchInvalidSearch                                                     ID = "search.invalid_search"
	SearchInvalidSearchPrepareFileSearch                                    ID = "search.invalid_search_prepare_file_search"
	SearchInvalidSearchPrepareFileSearchFileSearch                          ID = "search.invalid_search_prepare_file_search_file_search"
	SearchInvalidSearchPrepareFileSearchFileSearch8EDB05                    ID = "search.invalid_search_prepare_file_search_file_search8edb05"
	SearchInvalidSearchPrepareFileSearchFileSearch448466                    ID = "search.invalid_search_prepare_file_search_file_search448466"
	SelectquerySelectSourceTooLarge                                         ID = "selectquery.select_source_too_large"
	SelectquerySelectResultLimit                                            ID = "selectquery.select_result_limit"
	SelectquerySelectQueryFailed                                            ID = "selectquery.select_query_failed"
	SelectquerySelectClosed                                                 ID = "selectquery.select_closed"
	SelectquerySelectResultLimitQueryUsecase                                ID = "selectquery.select_result_limit_query_usecase"
	SelectquerySelectSourceChanged                                          ID = "selectquery.select_source_changed"
	SelectqueryInvalidSelectStatistics                                      ID = "selectquery.invalid_select_statistics"
	SelectqueryInvalidSelectQuery                                           ID = "selectquery.invalid_select_query"
	ServerMinIOHistoryIsUnavailable                                         ID = "server.minio_history_is_unavailable"
	ServerServerConfigChanged                                               ID = "server.server_config_changed"
	ServerInvalidConfigHistory                                              ID = "server.invalid_config_history"
	ServerInvalidConfigValues                                               ID = "server.invalid_config_values"
	ServerConfigEnvironmentOverride                                         ID = "server.config_environment_override"
	ServerInvalidConfigValue                                                ID = "server.invalid_config_value"
	ServerInvalidConfigKey                                                  ID = "server.invalid_config_key"
	ServerInvalidDiagnostic                                                 ID = "server.invalid_diagnostic"
	ServerMinIOEndedTheTraceBeforeCollectionFinished                        ID = "server.minio_ended_the_trace_before_collection_finished"
	ServerMinIOCouldNotOpenTheServerLog                                     ID = "server.minio_could_not_open_the_server_log"
	ServerNotImplemented                                                    ID = "server.not_implemented"
	ServerInvalidPoolOperation                                              ID = "server.invalid_pool_operation"
	ServerInvalidPoolOperationPoolChange                                    ID = "server.invalid_pool_operation_pool_change"
	ServerConfirmationRequired                                              ID = "server.confirmation_required"
	ServerPoolsChanged                                                      ID = "server.pools_changed"
	ServerPoolNotFound                                                      ID = "server.pool_not_found"
	ServerPoolsNotReady                                                     ID = "server.pools_not_ready"
	ServerPoolBusy                                                          ID = "server.pool_busy"
	ServerPoolBusyPoolChange                                                ID = "server.pool_busy_pool_change"
	ServerRebalanceInactive                                                 ID = "server.rebalance_inactive"
	ServerDecommissionInactive                                              ID = "server.decommission_inactive"
	ServerConcurrentRequests0MeansAutomatic                                 ID = "server.concurrent_requests0_means_automatic"
	ServerClusterResponseTimeout                                            ID = "server.cluster_response_timeout"
	ServerRemoteStorageTimeout                                              ID = "server.remote_storage_timeout"
	ServerReplicationPriority                                               ID = "server.replication_priority"
	ServerReplicationWorkers                                                ID = "server.replication_workers"
	ServerLargeObjectWorkers                                                ID = "server.large_object_workers"
	ServerStorageTierTransitionWorkers                                      ID = "server.storage_tier_transition_workers"
	ServerIncompleteUploadCleanupInterval                                   ID = "server.incomplete_upload_cleanup_interval"
	ServerIncompleteUploadExpirationAge                                     ID = "server.incomplete_upload_expiration_age"
	ServerDeletedObjectBackgroundCleanupInterval                            ID = "server.deleted_object_background_cleanup_interval"
	ServerScannerSpeed                                                      ID = "server.scanner_speed"
	ServerObjectVersionCountThreshold                                       ID = "server.object_version_count_threshold"
	ServerFolderCountThreshold                                              ID = "server.folder_count_threshold"
	ServerBitRotScanning                                                    ID = "server.bit_rot_scanning"
	ServerHealingPause                                                      ID = "server.healing_pause"
	ServerIOOperationThreshold                                              ID = "server.io_operation_threshold"
	ServerDriveWorkersEmptyMeansAutomatic                                   ID = "server.drive_workers_empty_means_automatic"
	ServerInvalidSubsystem                                                  ID = "server.invalid_subsystem"
	ServerServerFeatureUnavailable                                          ID = "server.server_feature_unavailable"
	ServerServerOperationFailed                                             ID = "server.server_operation_failed"
	ServerPermissionUnknown                                                 ID = "server.permission_unknown"
	ServerAccessDenied                                                      ID = "server.access_denied"
	ServerConfigTooLarge                                                    ID = "server.config_too_large"
	ServerInvalidServerConfig                                               ID = "server.invalid_server_config"
	ServerObjectStatisticsAreUnavailableScannerDataOr                       ID = "server.object_statistics_are_unavailable_scanner_data_or"
	ServerInvalidMetrics                                                    ID = "server.invalid_metrics"
	ServerPoolManagementIsOnlyAvailableInA                                  ID = "server.pool_management_is_only_available_ina"
	ServerCouldNotCheckRebalancingChangesAreBlocked                         ID = "server.could_not_check_rebalancing_changes_are_blocked"
	ServerUnknownPoolHealth                                                 ID = "server.unknown_pool_health"
	ServerUnhealthyPools                                                    ID = "server.unhealthy_pools"
	ServerUnhealthyPoolsServerPools                                         ID = "server.unhealthy_pools_server_pools"
	ServerUnknownPoolHealthServerPools                                      ID = "server.unknown_pool_health_server_pools"
	SessionsInvalidCredentials                                              ID = "sessions.invalid_credentials"
	StagingTemporaryStorageFull                                             ID = "staging.temporary_storage_full"
	StorageAdminResponseTooLarge                                            ID = "storage.admin_response_too_large"
	StorageConditionalDeleteUnavailable                                     ID = "storage.conditional_delete_unavailable"
	StorageInvalidDeletionCondition                                         ID = "storage.invalid_deletion_condition"
	StorageDeleteUncertain                                                  ID = "storage.delete_uncertain"
	StorageSourceChanged                                                    ID = "storage.source_changed"
	StorageAccessDenied                                                     ID = "storage.access_denied"
	StorageConditionalDeleteUnavailableConditional                          ID = "storage.conditional_delete_unavailable_conditional"
	StorageDeleteUncertainConditional                                       ID = "storage.delete_uncertain_conditional"
	StorageConditionalUploadConflict                                        ID = "storage.conditional_upload_conflict"
	StorageLifecycleTooLarge                                                ID = "storage.lifecycle_too_large"
	StorageCopyUnconfirmed                                                  ID = "storage.copy_unconfirmed"
	StorageInvalidCopyPartSize                                              ID = "storage.invalid_copy_part_size"
	StorageObjectTooLarge                                                   ID = "storage.object_too_large"
	StorageSourceChangedCopy                                                ID = "storage.source_changed_copy"
	StorageCopyPartUnconfirmed                                              ID = "storage.copy_part_unconfirmed"
	StorageCopyUnconfirmedCopy                                              ID = "storage.copy_unconfirmed_copy"
	StorageSourceChangedCopySourceError                                     ID = "storage.source_changed_copy_source_error"
	StorageDiagnosticRecordTooLarge                                         ID = "storage.diagnostic_record_too_large"
	StorageInvalidDiagnosticRecord                                          ID = "storage.invalid_diagnostic_record"
	StorageAccessDeniedDiagnosticProbe                                      ID = "storage.access_denied_diagnostic_probe"
	StorageProbeUnavailable                                                 ID = "storage.probe_unavailable"
	StorageMetricsTooLarge                                                  ID = "storage.metrics_too_large"
	StorageDirectUploadSigningUnavailable                                   ID = "storage.direct_upload_signing_unavailable"
	StorageInvalidMultipartPart                                             ID = "storage.invalid_multipart_part"
	StorageInvalidObjectAttributes                                          ID = "storage.invalid_object_attributes"
	StorageNoSuchVersion                                                    ID = "storage.no_such_version"
	StorageObjectAttributesUnsupported                                      ID = "storage.object_attributes_unsupported"
	StorageInvalidObjectAttributesResponse                                  ID = "storage.invalid_object_attributes_response"
	StorageS3ResponseTooLarge                                               ID = "storage.s3_response_too_large"
	StorageInvalidS3ListingResponse                                         ID = "storage.invalids3_listing_response"
	StorageS3WriteUnconfirmed                                               ID = "storage.s3_write_unconfirmed"
	StorageSelectUpstreamLimit                                              ID = "storage.select_upstream_limit"
	StorageInvalidSelectEvent                                               ID = "storage.invalid_select_event"
	StorageAccessDeniedSelectResponse                                       ID = "storage.access_denied_select_response"
	StorageNotImplemented                                                   ID = "storage.not_implemented"
	StorageSelectStreamFailed                                               ID = "storage.select_stream_failed"
	StorageSigningRegionRequired                                            ID = "storage.signing_region_required"
	StorageSigningNetworkDisabled                                           ID = "storage.signing_network_disabled"
	TiersTierOperationFailed                                                ID = "tiers.tier_operation_failed"
	TiersTierUsageUnknown                                                   ID = "tiers.tier_usage_unknown"
	TiersTierUsageUnknownTiers                                              ID = "tiers.tier_usage_unknown_tiers"
	TiersTierUsageUnknownTiersUnusedTier                                    ID = "tiers.tier_usage_unknown_tiers_unused_tier"
	TiersTierInUse                                                          ID = "tiers.tier_in_use"
	TiersInvalidTierName                                                    ID = "tiers.invalid_tier_name"
	TiersTierPermissionUnknown                                              ID = "tiers.tier_permission_unknown"
	TiersAccessDenied                                                       ID = "tiers.access_denied"
	TiersTierNotFound                                                       ID = "tiers.tier_not_found"
	TiersTierExists                                                         ID = "tiers.tier_exists"
	TiersInvalidTierType                                                    ID = "tiers.invalid_tier_type"
	TiersInvalidTierTarget                                                  ID = "tiers.invalid_tier_target"
	TiersTierEndpointNotAllowed                                             ID = "tiers.tier_endpoint_not_allowed"
	TiersTierSameServer                                                     ID = "tiers.tier_same_server"
	TiersTierChanged                                                        ID = "tiers.tier_changed"
	TiersTierConfirmation                                                   ID = "tiers.tier_confirmation"
	TiersInvalidTierCredentials                                             ID = "tiers.invalid_tier_credentials"
	TiersTierCredentialsUnsupported                                         ID = "tiers.tier_credentials_unsupported"
	TransfersInvalidTransfer                                                ID = "transfers.invalid_transfer"
	TransfersSameDestination                                                ID = "transfers.same_destination"
	TransfersSourceChanged                                                  ID = "transfers.source_changed"
	TransfersTransferTooLarge                                               ID = "transfers.transfer_too_large"
	TransfersTheCopyWasCreated                                              ID = "transfers.the_copy_was_created"
	TransfersTheFolderMarkerWasCopiedAndThe                                 ID = "transfers.the_folder_marker_was_copied_and_the"
	TransfersTheCopyWasCreatedButItsState                                   ID = "transfers.the_copy_was_created_but_its_state"
	TransfersTheDestinationChangedAfterCopyingTheSource                     ID = "transfers.the_destination_changed_after_copying_the_source"
	TransfersTheCopyWasCreatedButTheSource                                  ID = "transfers.the_copy_was_created_but_the_source"
	TransfersTheSourceFileChangedAfterCopyingAnd                            ID = "transfers.the_source_file_changed_after_copying_and"
	TransfersObjectDeleteDenied                                             ID = "transfers.object_delete_denied"
	TransfersDeleteUncertain                                                ID = "transfers.delete_uncertain"
	TransfersTheCopyWasCreatedAndSourceDeletion                             ID = "transfers.the_copy_was_created_and_source_deletion"
	TransfersTheCopyWasCreatedButTheSourceFileTransfer                      ID = "transfers.the_copy_was_created_but_the_source_file_transfer"
	TransfersTheCopyWasCreatedButTheSourceFileTransferTransfer              ID = "transfers.the_copy_was_created_but_the_source_file_transfer_transfer"
	TransfersInvalidTransferPrepareUsecase                                  ID = "transfers.invalid_transfer_prepare_usecase"
	UploadsInvalidUploadSize                                                ID = "uploads.invalid_upload_size"
	UploadsChecksumStagingUnavailable                                       ID = "uploads.checksum_staging_unavailable"
	UploadSizeMismatch                                                      ID = "uploads.size_mismatch"
	UploadInterrupted                                                       ID = "uploads.interrupted"
	UploadsInvalidKey                                                       ID = "uploads.invalid_key"
	UploadsImportNotPrepared                                                ID = "uploads.import_not_prepared"
	UploadsInvalidUploadPartsResponse                                       ID = "uploads.invalid_upload_parts_response"
	UploadsPartsLimit                                                       ID = "uploads.parts_limit"
	UploadsInvalidUploadListing                                             ID = "uploads.invalid_upload_listing"
	UploadsInvalidAbort                                                     ID = "uploads.invalid_abort"
	UploadsUploadChanged                                                    ID = "uploads.upload_changed"
	UploadsCouldNotConfirmTheAbortCheckThe                                  ID = "uploads.could_not_confirm_the_abort_check_the"
	UploadsTheAbortWasConfirmedTheCompletedObject                           ID = "uploads.the_abort_was_confirmed_the_completed_object"
	UploadsTheUploadIsStillAvailableCheckIts                                ID = "uploads.the_upload_is_still_available_check_its"
	UploadsInvalidUploadListingIncompleteUploadsUsecase                     ID = "uploads.invalid_upload_listing_incomplete_uploads_usecase"
	UploadsInvalidUploadCursor                                              ID = "uploads.invalid_upload_cursor"
	UploadsInvalidChecksum                                                  ID = "uploads.invalid_checksum"
	UploadsUnsupportedChecksumMode                                          ID = "uploads.unsupported_checksum_mode"
	UploadsUnsupportedChecksumAlgorithm                                     ID = "uploads.unsupported_checksum_algorithm"
	UploadsChecksumMismatch                                                 ID = "uploads.checksum_mismatch"
	UploadsInviteLimit                                                      ID = "uploads.invite_limit"
	UploadsInvalidInvite                                                    ID = "uploads.invalid_invite"
	UploadsInviteDenied                                                     ID = "uploads.invite_denied"
	UploadsMissingUploadParts                                               ID = "uploads.missing_upload_parts"
	UploadsTheSHA256COMPOSITEChecksumOfTheParts                             ID = "uploads.thesha256composite_checksum_of_the_parts"
	UploadsInvalidCompleteResponse                                          ID = "uploads.invalid_complete_response"
	UploadsMultipartNotFound                                                ID = "uploads.multipart_not_found"
	UploadsMultipartStateConflict                                           ID = "uploads.multipart_state_conflict"
	UploadsAccessDenied                                                     ID = "uploads.access_denied"
	UploadsConditionalCompletionUnverified                                  ID = "uploads.conditional_completion_unverified"
	UploadsInvalidBucketOrFilePath                                          ID = "uploads.invalid_bucket_or_file_path"
	UploadsTheFileSizeMustBeBetween0                                        ID = "uploads.the_file_size_must_be_between0"
	UploadsInvalidContentType                                               ID = "uploads.invalid_content_type"
	UploadsUpTo32MetadataEntriesAreAllowed                                  ID = "uploads.up_to32_metadata_entries_are_allowed"
	UploadsInvalidFileMetadata                                              ID = "uploads.invalid_file_metadata"
	UploadsMetadataMustNotExceed8KiB                                        ID = "uploads.metadata_must_not_exceed8_kib"
	UploadsUnsupportedChecksumModeCreate                                    ID = "uploads.unsupported_checksum_mode_create"
	UploadsMultipartExpired                                                 ID = "uploads.multipart_expired"
	UploadsUnsupportedChecksumModeParts                                     ID = "uploads.unsupported_checksum_mode_parts"
	UploadsUploadedPartSizeMismatch                                         ID = "uploads.uploaded_part_size_mismatch"
	UploadsPartChecksumUnavailable                                          ID = "uploads.part_checksum_unavailable"
	UploadsPartChanged                                                      ID = "uploads.part_changed"
	UploadsInvalidMultipartParts                                            ID = "uploads.invalid_multipart_parts"
	UploadsThePartNumberAndExactSizeMust                                    ID = "uploads.the_part_number_and_exact_size_must"
	UploadsPartContentConflict                                              ID = "uploads.part_content_conflict"
	UploadsTheStorageRejectedThePartSHA256Does                              ID = "uploads.the_storage_rejected_the_partsha256_does"
	UploadsThePartWasOnlyPartiallyUploadedRetrieve                          ID = "uploads.the_part_was_only_partially_uploaded_retrieve"
	UploadsTheUploadedPartSSHA256ChecksumDoes                               ID = "uploads.the_uploaded_partssha256_checksum_does"
	UploadsPartContentConflictParts                                         ID = "uploads.part_content_conflict_parts"
	UploadsMultipartExpiredParts                                            ID = "uploads.multipart_expired_parts"
	UploadsInvalidUpload                                                    ID = "uploads.invalid_upload"
	UploadsUploadExists                                                     ID = "uploads.upload_exists"
	UploadsInvalidSourceURL                                                 ID = "uploads.invalid_sourceurl"
	UploadsImportTooLarge                                                   ID = "uploads.import_too_large"
	UploadsIncompleteSource                                                 ID = "uploads.incomplete_source"
	UploadsIncompleteSourceStageRemote                                      ID = "uploads.incomplete_source_stage_remote"
	UploadsInvitePermissionsUnavailable                                     ID = "uploads.invite_permissions_unavailable"
	UploadsIncompleteUpload                                                 ID = "uploads.incomplete_upload"
	UploadsChecksumMismatchWriteUsecase                                     ID = "uploads.checksum_mismatch_write_usecase"
	ValidationInvalidMembers                                                ID = "validation.invalid_members"
	ValidationInvalidMembersManagement                                      ID = "validation.invalid_members_management"
	ValidationInvalidPolicy                                                 ID = "validation.invalid_policy"
	ValidationInvalidPolicyObjects                                          ID = "validation.invalid_policy_objects"
)

// AllIDs returns a detached inventory for catalog validation.
func AllIDs() []ID {
	return []ID{
		CommandInvalid, CommandConflict, CommandRunning, CommandUnknown, CommandUnavailable, CommandLimit,
		OperationDisabled, AuthStateUnavailable, AuthStateInvalid, AuthStateDenied, AuthStateNotFound, AuthStateConflict, AuthStateLimit, ResourceNameInvalid, Combined, ManagementUnsupported, RouteNotFound, HTTPStopping,
		HttpInvalidBearerToken,
		SessionsInvalidTokenTTL,
		HttpMethodNotAllowed,
		HttpInvalidPermissionQueries,
		HttpInvalidPermissionQueriesActionAccess,
		HttpInvalidPermissionQueriesActionAccessHandleActionAccess,
		HttpInvalidObjectAttributes,
		HttpInvalidObjectAttributesObjectAttributes,
		HttpMethodNotAllowedBucketUpdate,
		HttpInvalidFolderDelete,
		HttpInvalidFolderResult,
		HttpInvalidMultipartID,
		HttpInvalidPartNumber,
		HttpInvalidPartSize,
		HttpInvalidDelete,
		HttpInvalidSelectQuery,
		HttpOriginDenied,
		HttpInvalidTierOperation,
		HttpInvalidUploadSize,
		HttpOriginDeniedUploadActions,
		HttpInvalidUpload,
		InternalError,
		HttpSessionExpired,
		HttpSessionExpiredSession,
		HttpCSRFDenied,
		HttpConfirmationRequired,
		HttpJSONRequired,
		HttpInvalidJSON,
		HttpInvalidJSONResponse,
		AccessAccessDenied,
		ApperrorCouldNotVerifyTheCredentialsCheckThe,
		ApperrorInsufficientMinIOPermissionsForThisOperation,
		ApperrorTheResourceNoLongerExistsRefreshThe,
		ApperrorTheBucketStateDoesNotAllowThis,
		ApperrorTheBucketIsNotEmptyThisOperation,
		ApperrorTheStorageRejectedTheDataBecauseThe,
		ApperrorTheStorageRejectedTheChecksumFormatCheck,
		ApperrorTheResourceAlreadyExistsOrIsIn,
		ApperrorMinIORejectedTheParametersCheckTheName,
		ApperrorTheMinIOTokenIsInvalidOrExpired,
		ApperrorThisServerVersionOrTypeDoesNot,
		ApperrorTheS3APIDidNotRespondIn,
		ApperrorCouldNotCompleteTheMinIORequestCheck,
		ArchivesArchiveExpired,
		ArchivesArchiveStarted,
		ArchivesTheFileChangedAfterVerificationPrepareA,
		ArchivesCouldNotReadTheEntireFileDo,
		ArchivesInvalidGzip,
		ArchivesInvalidGzipGzipView,
		ArchivesGzipTextTooLarge,
		ArchivesGzipTooManyLines,
		ArchivesNotUTF8Text,
		ArchivesNotGzip,
		ArchivesGzipTooLarge,
		ArchivesObjectChanged,
		ArchivesArchiveStartedModel,
		ArchivesInvalidArchiveBase,
		ArchivesArchiveRunning,
		ArchivesTheTransferWasNotStarted,
		ArchivesEmptyArchive,
		ArchivesArchivePartialConsent,
		ArchivesZIPCompletionWasNotConfirmedPrepareA,
		ArchivesTheTransferWasStoppedTheZIPMay,
		ArchivesInvalidArchiveName,
		ArchivesInvalidArchiveBasePaths,
		ArchivesInvalidArchiveBasePathsArchivePaths,
		ArchivesArchivePathConflict,
		ArchivesInvalidArchive,
		ArchivesInvalidArchivePrepareUsecase,
		ArchivesInvalidArchivePrepareUsecasePrepareArchive,
		ArchivesInvalidArchivePrepareUsecasePrepareArchiveD520E6,
		ArchivesInvalidArchivePrepareUsecasePrepareArchive4AEA25,
		ArchivesInvalidArchivePrepareUsecasePrepareArchive0BA8AF,
		ArchivesInvalidArchivePrepareUsecasePrepareArchive3EDEA0,
		ArchivesArchiveTooLarge,
		ArchivesUnsafeArchivePath,
		ArchivesThisPathCannotBeSafelyStoredIn,
		ArchivesArchiveTooLargePrepareUsecase,
		ArchivesEmptyArchivePrepareUsecase,
		ArchivesUnsafePath,
		ArchivesSymbolicLink,
		ArchivesTheArchiveIsEncrypted,
		ArchivesTheCompressionMethodIsNotSupported,
		ArchivesTheFileExceeds256MiB,
		ArchivesInvalidDestination,
		ArchivesZIPChanged,
		ArchivesZIPDirectory,
		ArchivesCorruptZIPEntry,
		ArchivesCorruptZIPEntryZipExplorerUsecase,
		ArchivesZIPClosed,
		ArchivesInvalidZIPEntry,
		ArchivesInvalidZIP,
		ArchivesZIPLimit,
		ArchivesInvalidZIPReader,
		ArchivesZIPLimitReader,
		ArchivesInvalidZIPReaderCheckedZIP,
		ArchivesInvalidZIPReaderCheckedZIPD112EC,
		ArchivesInvalidZIPReaderCheckedZIP9AD98F,
		ArchivesZIPLimitReaderLoadStoredZIP,
		ArchivesZIPChangedReader,
		BatchInvalidBatch,
		BatchConfirmationRequired,
		BatchInvalidBatchParameters,
		BucketsCouldNotReadPrincipal,
		BucketsInvalidPolicyDefinition,
		BucketsInvalidBucketName,
		BucketsCouldNotParseOneOfTheIAM,
		BucketsShowingAssignmentsForTheFirst1000,
		BucketsIAMPolicyAssignmentsWereOnlyPartiallyRead,
		BucketsUsersWereOnlyPartiallyRead,
		BucketsGroupsWereOnlyPartiallyRead,
		BucketsCheckedTheFirst500Groups,
		BucketsTheGroupCheckTimedOut,
		BucketsCouldNotRetrieveMembersAndStatusFor,
		BucketsDirect,
		BucketsGroup,
		Buckets,
		BucketsMemberOfDisabledGroups,
		BucketsApplicationKeysAreUnavailable,
		BucketsOwner,
		BucketsTheKeyRestrictionsCouldNotBeRead,
		BucketsCouldNotReadAllKeyRestrictions,
		BucketsTheKeyOwnerChangedRefreshTheDetails,
		BucketsAKeyOwnerChangedDuringTheCheck,
		BucketsCouldNotReadTheRestrictionPolicyFor,
		BucketsTheOwnerBelongsToDisabledGroups,
		BucketsTheOwnerUserIsDisabled,
		BucketsTheOwnerSPermissionsAreNotListed,
		BucketsCheckedTheFirst500ApplicationKeys,
		BucketsTheBucketPolicyCouldNotBeRead,
		BucketsTheBucketPolicyHasAnUnsupportedFormat,
		BucketsBucketPolicy,
		BucketsPrincipalIsEvaluatedByMinIO,
		BucketsTheseRulesMayAllowAnonymousAccessOr,
		BucketsThisSettingContainsParametersThatTheCurrent,
		BucketsInvalidCORS,
		BucketsBucketSettingChanged,
		BucketsCORSRuleNotFound,
		BucketsInvalidCORSBucketBrowserSettingsChange,
		BucketsInvalidCORSBucketBrowserSettingsChangeChangeBrowserSetting,
		BucketsCORSDuplicateID,
		BucketsConfirmationRequired,
		BucketsInvalidEncryption,
		BucketsConfirmationRequiredBucketBrowserSettingsChange,
		BucketsInvalidEncryptionBucketBrowserSettingsChange,
		BucketsInvalidEncryptionBucketBrowserSettingsChangeChangeBrowserSetting,
		BucketsKMSUnavailable,
		BucketsInvalidBucketSetting,
		BucketsBucketSettingWriteFailed,
		BucketsObjectTag,
		BucketsSizeGreaterThan,
		BucketsSizeLessThan,
		BucketsAbortIncompleteUploadsAfter,
		BucketsDeleteAllVersionsWhenTheCurrentVersion,
		BucketsDeleteAllVersionsAfter,
		BucketsDeleteTheMarkerAndItsVersionsAfter,
		BucketsCompressTheCurrentVersion,
		BucketsCompressOlderVersions,
		BucketsTheConfigurationContainsParametersThatTheCurrent,
		BucketsTheRuleNameMustContain1To,
		BucketsThePathMustBeAtMost1,
		BucketsPeriodsAndRetainedVersionCountsMustBe,
		BucketsChooseAnAgeInDaysADate,
		BucketsSpecifyAValidTransitionAgeInDays,
		BucketsATierNameMustContain3To,
		BucketsSpecifyATransitionDateOrAnAge,
		BucketsSelectATierForCurrentVersions,
		BucketsTheTransitionAgeForOlderVersionsMust,
		BucketsSelectATierForOlderVersions,
		BucketsSpecifyAValidExpirationDateInYYYY,
		BucketsAddAtLeastOneActionDeletingVersions,
		BucketsBucketSettingChangedBucketLifecycleChange,
		BucketsLifecycleRuleNotFound,
		BucketsInvalidLifecycleRule,
		BucketsLifecycleLimit,
		BucketsInvalidLifecycleRuleBucketLifecycleChange,
		BucketsConfirmationRequiredBucketLifecycleChange,
		BucketsMethodNotAllowed,
		BucketsInvalidLifecycleRuleBucketLifecycleChangeChangeLifecycle,
		BucketsAccessDenied,
		BucketsLifecycleWriteFailed,
		BucketsInvalidRetention,
		BucketsInvalidRetentionBucketRetention,
		BucketsInvalidRetentionBucketRetentionParseRetention,
		BucketsBucketSettingChangedBucketUpdate,
		BucketsInvalidTags,
		BucketsInvalidTagsBucketUpdate,
		BucketsInvalidTagsBucketUpdateValidateBucketTags,
		BucketsInvalidTagsBucketUpdateValidateBucketTagsB75523,
		BucketsInvalidBucketNameCatalogChange,
		BucketsAlreadyExists,
		BucketsInvalidBucketField,
		BucketsInvalidVersioning,
		BucketsInvalidObjectLock,
		BucketsVersioningRestricted,
		BucketsVersioningRequired,
		BucketsObjectLockUnavailable,
		BucketsInvalidPolicy,
		BucketsInvalidPolicySettingsChange,
		BucketsInvalidQuota,
		CapabilitiesInvalidCapability,
		CapabilitiesInvalidCapabilityScope,
		CapabilitiesInvalidCapabilityScopeReportUsecase,
		EndpointInvalidEndpoint,
		EndpointInvalidEndpointEndpoint,
		EndpointEndpointNotAllowed,
		EndpointEndpointUnavailable,
		EndpointEndpointNotAllowedEndpoint,
		FilesInvalidContentSearch,
		FilesInvalidContentExtensions,
		FilesInvalidUsageScope,
		FilesInvalidUsageCursor,
		FilesUsageVersionsUnavailable,
		FilesInvalidUsagePage,
		FilesInvalidUsagePageObjectUsageUsecase,
		FilesInvalidUsagePageObjectUsageUsecaseReadObjectUsage,
		FilesInvalidVersion,
		FilesInvalidProtection,
		FilesConfirmationRequired,
		FilesInvalidHold,
		FilesInvalidRetention,
		FilesObjectProtected,
		FilesInvalidListing,
		FilesUnsupportedPreview,
		FilesPreviewTooLarge,
		FilesObjectChanged,
		FilesObjectChangedObjectsUsecase,
		FilesUnsupportedMedia,
		FilesInvalidLifetime,
		FilesInvalidMode,
		FoldersInvalidFolderPath,
		FoldersInvalidFolderListing,
		FoldersInvalidFolderListingFolderDelete,
		FoldersFolderDeleteLimit,
		FoldersInvalidFolderDelete,
		FoldersConfirmationRequired,
		FoldersRevisionRequired,
		FoldersObjectChanged,
		FoldersFolderNotEmpty,
		FoldersObjectDeleteDenied,
		FoldersInvalidRange,
		FoldersInvalidFolderName,
		FoldersInvalidFolderPathFolders,
		FoldersInvalidFolderPathFoldersFolderKey,
		FoldersInvalidFolderNameFolders,
		FoldersFolderExists,
		FoldersFolderShareExpired,
		FoldersFolderScopeDenied,
		FoldersFolderScopeDeniedPublicUsecase,
		FoldersInvalidTicket,
		FoldersUnsupportedPreview,
		FoldersUnsupportedPreviewPublicUsecase,
		FoldersPreviewTooLarge,
		FoldersInvalidText,
		FoldersNotFound,
		FoldersDownloadExpired,
		FoldersFolderScopeDeniedPublicUsecaseOpenSharedFile,
		FoldersFolderShareExpiredModel,
		FoldersSharedReadClosed,
		FoldersInvalidFolderShare,
		FoldersSharePermissionsUnavailable,
		FoldersFolderShareDenied,
		IamMinIODidNotReturnActionPermissionsIn,
		IamMinIODeniedReadingActionPermissionsModificationButtons,
		IamTheServerDoesNotSupportCheckingAction,
		IamCouldNotRetrieveActionPermissionsFromMinIO,
		IamMinIODidNotReturnTheConnectionOwner,
		IamCouldNotReadTheIAMPolicyFormat,
		IamCouldNotConfirmTheKeyOwnerModification,
		IamMinIOReportsThatTheKeyIsDisabled,
		IamCouldNotReadTheKeyRestrictionsModification,
		IamCouldNotReadMinIOSConnectionOwner,
		IamTheMinIOPermissionsResponseIsTooLarge,
		IamTheMinIOAddressOrProxyDoesNot,
		IamMinIOOrItsProxyReturnedAnError,
		IdentitiesInvalidTarget,
		IdentitiesInvalidTargetBindings,
		IdentitiesInvalidBinding,
		IdentitiesInvalidGroup,
		IdentitiesAlreadyExists,
		IdentitiesGroupCreateAcknowledgementRequired,
		IdentitiesInvalidStatus,
		IdentitiesGroupNotEmpty,
		IdentitiesTheDetailsWereNotRetrievedWithinThe,
		IdentitiesInvalidPolicyDefinition,
		IdentitiesInvalidPolicyName,
		IdentitiesAlreadyExistsPolicies,
		IdentitiesPolicyCreateAcknowledgementRequired,
		IdentitiesPolicyChanged,
		IdentitiesPolicyInUse,
		IdentitiesTheAssignmentsWereNotRetrievedWithinThe,
		IdentitiesTheDatesWereNotRetrievedWithinThe,
		IdentitiesInvalidUser,
		IdentitiesAlreadyExistsUsers,
		IdentitiesUserCreateAcknowledgementRequired,
		IdentitiesCurrentUser,
		IdentitiesCurrentUserUsers,
		KeysAccessKeyNotFound,
		KeysUnreadableAccessKeyPolicy,
		KeysInvalidKeyName,
		KeysInvalidKeyDescription,
		KeysInvalidParent,
		KeysInvalidPolicy,
		KeysInvalidPolicyAccessKeys,
		KeysInvalidPolicyAccessKeysValidateKeyInput,
		KeysInvalidExpiration,
		KeysAccessKeyChanged,
		KeysImmutableParent,
		KeysEmptyKeyMetadata,
		KeysInvalidScope,
		KeysCurrentAccessKey,
		KeysConfirmationRequired,
		ObjectsChecksumStarted,
		ObjectsChecksumClosed,
		ObjectsChecksumNotPrepared,
		ObjectsInvalidObject,
		ObjectsInvalidExpectedChecksum,
		ObjectsTheStorageDidNotReturnChecksumsYou,
		ObjectsIncompleteChecksumRead,
		ObjectsObjectChanged,
		ObjectsInvalidVersion,
		ObjectsInvalidObjectAttributes,
		ObjectsInvalidAttributesSnapshot,
		ObjectsAttributesSnapshotRequired,
		ObjectsObjectAttributesChanged,
		ObjectsAttributesSnapshotUnavailable,
		ObjectsNotEditable,
		ObjectsTextTooLarge,
		ObjectsEncodedText,
		ObjectsObjectChangedObjectContentUsecase,
		ObjectsInvalidText,
		ObjectsInvalidTextObjectContentUsecase,
		ObjectsInvalidDocument,
		ObjectsContentChanged,
		ObjectsContentProtected,
		ObjectsConfirmationRequired,
		ObjectsRevisionRequired,
		ObjectsObjectChangedObjectDeleteUsecase,
		ObjectsObjectDeleteDenied,
		ObjectsTagsAreUnavailableCheckPermissionToRead,
		ObjectsEditingMetadataForThisEncryptionTypeIs,
		ObjectsMetadataOfAProtectedObjectIsRead,
		ObjectsInvalidPropertyMode,
		ObjectsInvalidObjectHeaders,
		ObjectsInvalidObjectHeadersObjectProperties,
		ObjectsInvalidObjectMetadata,
		ObjectsInvalidObjectMetadataObjectProperties,
		ObjectsInvalidObjectMetadataObjectPropertiesValidatePropertyPatch,
		ObjectsInvalidObjectTags,
		ObjectsInvalidObjectObjectPropertiesUsecase,
		ObjectsObjectPropertiesChanged,
		ObjectsEmptyPropertyPatch,
		ObjectsObjectPropertiesChangedObjectPropertyTarget,
		ObjectsObjectChangedObjectPropertyTarget,
		ObjectsObjectPropertiesChangedObjectPropertyTargetTaggedTarget,
		ObjectsObjectTagsUnverified,
		ObjectsCopyUnsupported,
		ObjectsInvalidCursor,
		ObjectsInvalidCursorObjectVersions,
		ObjectsVersionListingFailed,
		ObjectsVersionListingFailedObjectVersions,
		ObjectsInvalidObjectObjects,
		ObjectsDestinationChanged,
		ObjectsDestinationChangedConditional,
		RemotesourceSourceResponse,
		RemotesourceSourceUnavailable,
		RemotesourceSourceDenied,
		RemotesourceSourceDNS,
		RemotesourceSourceDeniedClient,
		RemotesourceTooManyRedirects,
		RuntimeAccountInfoTooLarge,
		RuntimeTransferLimit,
		RuntimeLoginRateLimit,
		RuntimeTooManyRequests,
		RuntimeLoginRateLimitAdmission,
		RuntimeArchiveLimit,
		RuntimeCapabilityBusy,
		RuntimeInviteLimit,
		RuntimeInviteExists,
		RuntimeInviteNotFound,
		RuntimeMultipartLimit,
		RuntimeMultipartExists,
		RuntimeServerClosing,
		RuntimeSessionExpired,
		RuntimeSessionLimit,
		RuntimeShareLimit,
		RuntimeTicketLimit,
		RuntimeZIPBusy,
		SearchEncodedText,
		SearchSearchFileTooLarge,
		SearchSearchObjectChanged,
		SearchInvalidObjectListing,
		SearchInvalidSearch,
		SearchInvalidSearchPrepareFileSearch,
		SearchInvalidSearchPrepareFileSearchFileSearch,
		SearchInvalidSearchPrepareFileSearchFileSearch8EDB05,
		SearchInvalidSearchPrepareFileSearchFileSearch448466,
		SelectquerySelectSourceTooLarge,
		SelectquerySelectResultLimit,
		SelectquerySelectQueryFailed,
		SelectquerySelectClosed,
		SelectquerySelectResultLimitQueryUsecase,
		SelectquerySelectSourceChanged,
		SelectqueryInvalidSelectStatistics,
		SelectqueryInvalidSelectQuery,
		ServerMinIOHistoryIsUnavailable,
		ServerServerConfigChanged,
		ServerInvalidConfigHistory,
		ServerInvalidConfigValues,
		ServerConfigEnvironmentOverride,
		ServerInvalidConfigValue,
		ServerInvalidConfigKey,
		ServerInvalidDiagnostic,
		ServerMinIOEndedTheTraceBeforeCollectionFinished,
		ServerMinIOCouldNotOpenTheServerLog,
		ServerNotImplemented,
		ServerInvalidPoolOperation,
		ServerInvalidPoolOperationPoolChange,
		ServerConfirmationRequired,
		ServerPoolsChanged,
		ServerPoolNotFound,
		ServerPoolsNotReady,
		ServerPoolBusy,
		ServerPoolBusyPoolChange,
		ServerRebalanceInactive,
		ServerDecommissionInactive,
		ServerConcurrentRequests0MeansAutomatic,
		ServerClusterResponseTimeout,
		ServerRemoteStorageTimeout,
		ServerReplicationPriority,
		ServerReplicationWorkers,
		ServerLargeObjectWorkers,
		ServerStorageTierTransitionWorkers,
		ServerIncompleteUploadCleanupInterval,
		ServerIncompleteUploadExpirationAge,
		ServerDeletedObjectBackgroundCleanupInterval,
		ServerScannerSpeed,
		ServerObjectVersionCountThreshold,
		ServerFolderCountThreshold,
		ServerBitRotScanning,
		ServerHealingPause,
		ServerIOOperationThreshold,
		ServerDriveWorkersEmptyMeansAutomatic,
		ServerInvalidSubsystem,
		ServerServerFeatureUnavailable,
		ServerServerOperationFailed,
		ServerPermissionUnknown,
		ServerAccessDenied,
		ServerConfigTooLarge,
		ServerInvalidServerConfig,
		ServerObjectStatisticsAreUnavailableScannerDataOr,
		ServerInvalidMetrics,
		ServerPoolManagementIsOnlyAvailableInA,
		ServerCouldNotCheckRebalancingChangesAreBlocked,
		ServerUnknownPoolHealth,
		ServerUnhealthyPools,
		ServerUnhealthyPoolsServerPools,
		ServerUnknownPoolHealthServerPools,
		SessionsInvalidCredentials,
		StagingTemporaryStorageFull,
		StorageAdminResponseTooLarge,
		StorageConditionalDeleteUnavailable,
		StorageInvalidDeletionCondition,
		StorageDeleteUncertain,
		StorageSourceChanged,
		StorageAccessDenied,
		StorageConditionalDeleteUnavailableConditional,
		StorageDeleteUncertainConditional,
		StorageConditionalUploadConflict,
		StorageLifecycleTooLarge,
		StorageCopyUnconfirmed,
		StorageInvalidCopyPartSize,
		StorageObjectTooLarge,
		StorageSourceChangedCopy,
		StorageCopyPartUnconfirmed,
		StorageCopyUnconfirmedCopy,
		StorageSourceChangedCopySourceError,
		StorageDiagnosticRecordTooLarge,
		StorageInvalidDiagnosticRecord,
		StorageAccessDeniedDiagnosticProbe,
		StorageProbeUnavailable,
		StorageMetricsTooLarge,
		StorageDirectUploadSigningUnavailable,
		StorageInvalidMultipartPart,
		StorageInvalidObjectAttributes,
		StorageNoSuchVersion,
		StorageObjectAttributesUnsupported,
		StorageInvalidObjectAttributesResponse,
		StorageS3ResponseTooLarge,
		StorageInvalidS3ListingResponse,
		StorageS3WriteUnconfirmed,
		StorageSelectUpstreamLimit,
		StorageInvalidSelectEvent,
		StorageAccessDeniedSelectResponse,
		StorageNotImplemented,
		StorageSelectStreamFailed,
		StorageSigningRegionRequired,
		StorageSigningNetworkDisabled,
		TiersTierOperationFailed,
		TiersTierUsageUnknown,
		TiersTierUsageUnknownTiers,
		TiersTierUsageUnknownTiersUnusedTier,
		TiersTierInUse,
		TiersInvalidTierName,
		TiersTierPermissionUnknown,
		TiersAccessDenied,
		TiersTierNotFound,
		TiersTierExists,
		TiersInvalidTierType,
		TiersInvalidTierTarget,
		TiersTierEndpointNotAllowed,
		TiersTierSameServer,
		TiersTierChanged,
		TiersTierConfirmation,
		TiersInvalidTierCredentials,
		TiersTierCredentialsUnsupported,
		TransfersInvalidTransfer,
		TransfersSameDestination,
		TransfersSourceChanged,
		TransfersTransferTooLarge,
		TransfersTheCopyWasCreated,
		TransfersTheFolderMarkerWasCopiedAndThe,
		TransfersTheCopyWasCreatedButItsState,
		TransfersTheDestinationChangedAfterCopyingTheSource,
		TransfersTheCopyWasCreatedButTheSource,
		TransfersTheSourceFileChangedAfterCopyingAnd,
		TransfersObjectDeleteDenied,
		TransfersDeleteUncertain,
		TransfersTheCopyWasCreatedAndSourceDeletion,
		TransfersTheCopyWasCreatedButTheSourceFileTransfer,
		TransfersTheCopyWasCreatedButTheSourceFileTransferTransfer,
		TransfersInvalidTransferPrepareUsecase,
		UploadsInvalidUploadSize,
		UploadsChecksumStagingUnavailable,
		UploadSizeMismatch,
		UploadInterrupted,
		UploadsInvalidKey,
		UploadsImportNotPrepared,
		UploadsInvalidUploadPartsResponse,
		UploadsPartsLimit,
		UploadsInvalidUploadListing,
		UploadsInvalidAbort,
		UploadsUploadChanged,
		UploadsCouldNotConfirmTheAbortCheckThe,
		UploadsTheAbortWasConfirmedTheCompletedObject,
		UploadsTheUploadIsStillAvailableCheckIts,
		UploadsInvalidUploadListingIncompleteUploadsUsecase,
		UploadsInvalidUploadCursor,
		UploadsInvalidChecksum,
		UploadsUnsupportedChecksumMode,
		UploadsUnsupportedChecksumAlgorithm,
		UploadsChecksumMismatch,
		UploadsInviteLimit,
		UploadsInvalidInvite,
		UploadsInviteDenied,
		UploadsMissingUploadParts,
		UploadsTheSHA256COMPOSITEChecksumOfTheParts,
		UploadsInvalidCompleteResponse,
		UploadsMultipartNotFound,
		UploadsMultipartStateConflict,
		UploadsAccessDenied,
		UploadsConditionalCompletionUnverified,
		UploadsInvalidBucketOrFilePath,
		UploadsTheFileSizeMustBeBetween0,
		UploadsInvalidContentType,
		UploadsUpTo32MetadataEntriesAreAllowed,
		UploadsInvalidFileMetadata,
		UploadsMetadataMustNotExceed8KiB,
		UploadsUnsupportedChecksumModeCreate,
		UploadsMultipartExpired,
		UploadsUnsupportedChecksumModeParts,
		UploadsUploadedPartSizeMismatch,
		UploadsPartChecksumUnavailable,
		UploadsPartChanged,
		UploadsInvalidMultipartParts,
		UploadsThePartNumberAndExactSizeMust,
		UploadsPartContentConflict,
		UploadsTheStorageRejectedThePartSHA256Does,
		UploadsThePartWasOnlyPartiallyUploadedRetrieve,
		UploadsTheUploadedPartSSHA256ChecksumDoes,
		UploadsPartContentConflictParts,
		UploadsMultipartExpiredParts,
		UploadsInvalidUpload,
		UploadsUploadExists,
		UploadsInvalidSourceURL,
		UploadsImportTooLarge,
		UploadsIncompleteSource,
		UploadsIncompleteSourceStageRemote,
		UploadsInvitePermissionsUnavailable,
		UploadsIncompleteUpload,
		UploadsChecksumMismatchWriteUsecase,
		ValidationInvalidMembers,
		ValidationInvalidMembersManagement,
		ValidationInvalidPolicy,
		ValidationInvalidPolicyObjects,
	}
}

const (
	ManagementUnsupported ID = "http.management_unsupported"
	RouteNotFound         ID = "http.route_not_found"
)

const ResourceNameInvalid ID = "http.resource_name_invalid"

const HTTPStopping ID = "http.stopping"
