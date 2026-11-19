package access

import (
	"testing"

	"github.com/horrible-gh/TimeWeaver/internal/store"
)

// Each backend runs the same human-access contract against its actual SQL engine.
type storeAccessFixture func(*testing.T) *store.Store
type initializedAccessFixture func(*testing.T) (*store.Store, *Service)
type adminAccessFixture func(*testing.T) (*Service, Principal, string)
type osAccessFixture func(*testing.T) (*Service, Principal, string, *linkedOSProvider)

func TestSQLiteOSIdentityIsolationAndExplicitGrants(t *testing.T) {
	testOSIdentityIsolationAndExplicitGrants(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSManagementBindingAndPasswordBoundaries(t *testing.T) {
	testOSManagementBindingAndPasswordBoundaries(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSMissingAdminTargetsPreserveSession(t *testing.T) {
	testOSMissingAdminTargetsPreserveSession(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSReleasedIdentityGetsDistinctGeneratedUser(t *testing.T) {
	testOSReleasedIdentityGetsDistinctGeneratedUser(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSSessionChecksIdentityProviderAndExpiry(t *testing.T) {
	testOSSessionChecksIdentityProviderAndExpiry(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSLateAuthenticationCannotRestoreChangedBinding(t *testing.T) {
	testOSLateAuthenticationCannotRestoreChangedBinding(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSAdminCannotReplaceLocalRecoveryAdmin(t *testing.T) {
	testOSAdminCannotReplaceLocalRecoveryAdmin(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSCanonicalAccountRateLimitAndAuditRollback(t *testing.T) {
	testOSCanonicalAccountRateLimitAndAuditRollback(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSAccessV2UpgradePreservesLocalSessionsAndRemovedGrants(t *testing.T) {
	testOSAccessV2UpgradePreservesLocalSessionsAndRemovedGrants(t, sqliteLinkedOSFixture)
}

func TestSQLiteOSHTTPMethodsTransportAndNoFallback(t *testing.T) {
	testOSHTTPMethodsTransportAndNoFallback(t, sqliteLinkedOSFixture)
}

func TestSQLiteAdministrationAndRevocation(t *testing.T) {
	testAdministrationAndRevocation(t, sqliteAdminFixture)
}

func TestSQLiteConcurrentLastAdminProtection(t *testing.T) {
	testConcurrentLastAdminProtection(t, sqliteAdminFixture)
}

func TestSQLiteGroupRemovalCannotGrantGlobalRights(t *testing.T) {
	testGroupRemovalCannotGrantGlobalRights(t, sqliteAdminFixture)
}

func TestSQLiteAdministrationAuditFailureIsAtomic(t *testing.T) {
	testAdministrationAuditFailureIsAtomic(t, sqliteAdminFixture)
}

func TestSQLiteAccessV1UpgradePreservesSessionsAndRemovedGrants(t *testing.T) {
	testAccessV1UpgradePreservesSessionsAndRemovedGrants(t, sqliteAdminFixture)
}

func TestSQLiteAccessMigrationPreservesAndDoesNotRegrant(t *testing.T) {
	testAccessMigrationPreservesAndDoesNotRegrant(t, sqliteAccessStore)
}

func TestSQLiteAccessSessionLifecycleAndPasswordGate(t *testing.T) {
	testAccessSessionLifecycleAndPasswordGate(t, sqliteInitialized)
}

func TestSQLiteAccessExpiryRevocationAndLimits(t *testing.T) {
	testAccessExpiryRevocationAndLimits(t, sqliteInitialized)
}

func TestSQLiteAccessHTTPAndTransport(t *testing.T) {
	testAccessHTTPAndTransport(t, sqliteInitialized)
}

func TestSQLiteAccessSessionCapAndAtomicPasswordFailure(t *testing.T) {
	testAccessSessionCapAndAtomicPasswordFailure(t, sqliteInitialized)
}
