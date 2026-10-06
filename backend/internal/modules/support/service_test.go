package support

import (
	"testing"
	"time"
)

func TestSupportInputEnums(t *testing.T) {
	for _, value := range []string{CategoryPayments, CategoryWithdrawals, CategoryKYC, CategoryTechnical, CategoryBilling, CategoryOther} {
		if !validCategory(value) {
			t.Errorf("category %q should be valid", value)
		}
	}
	for _, value := range []string{StatusOpen, StatusPending, StatusInProgress, StatusResolved, StatusClosed} {
		if !validStatus(value) {
			t.Errorf("status %q should be valid", value)
		}
	}
	for _, value := range []string{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent} {
		if !validPriority(value) {
			t.Errorf("priority %q should be valid", value)
		}
	}
	if validCategory("tip_jar") || validStatus("done") || validPriority("merchant_set") {
		t.Fatal("unsupported ticket values must be rejected")
	}
}

func TestSLABreachExcludesResolvedTickets(t *testing.T) {
	service := &Service{slaAge: 24 * time.Hour}
	old := time.Now().Add(-48 * time.Hour)
	if !service.isSLABreach(Ticket{Status: StatusOpen, CreatedAt: old}) {
		t.Fatal("old open ticket should breach SLA")
	}
	if service.isSLABreach(Ticket{Status: StatusResolved, CreatedAt: old}) {
		t.Fatal("resolved ticket should not breach SLA")
	}
	if service.isSLABreach(Ticket{Status: StatusClosed, CreatedAt: old}) {
		t.Fatal("closed ticket should not breach SLA")
	}
}
