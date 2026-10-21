package ports

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSalesRequiresEveryUpstreamGuarantee(t *testing.T) {
	for _, c := range []LicensingCapabilities{{}, {AbsoluteValidity: true, PreserveUsage: true, IdempotentRecovery: true}, {AbsoluteValidity: true, IdempotentRecovery: true, CompareAndSwap: true}} {
		if !errors.Is(c.ValidateSales(), ErrContractUnsupported) {
			t.Fatal("incomplete upstream accepted")
		}
	}
	if (LicensingCapabilities{AbsoluteValidity: true, PreserveUsage: true, IdempotentRecovery: true, CompareAndSwap: true}).ValidateSales() != nil {
		t.Fatal("complete contract rejected")
	}
}

func TestEntitlementCommandRejectsStaleOrUnsafeTransitions(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	command := EntitlementCommand{
		OperationID: id, CustomerRef: strings.Repeat("r", 32), ProductID: id, PolicyID: id,
		ExpectedVersion: 0, TargetVersion: 1, PolicyVersion: 1, SnapshotHash: strings.Repeat("a", 64),
		ValidFrom: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), DesiredStatus: "active",
	}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*EntitlementCommand){
		func(c *EntitlementCommand) { c.TargetVersion = 0 },
		func(c *EntitlementCommand) { c.CustomerRef = "customer@example.com" },
		func(c *EntitlementCommand) { c.DesiredStatus = "revoked" },
		func(c *EntitlementCommand) { c.ExpectedVersion = 2; c.TargetVersion = 3 },
		func(c *EntitlementCommand) { c.ValidUntil = &c.ValidFrom },
	} {
		bad := command
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe command accepted")
		}
	}
}
