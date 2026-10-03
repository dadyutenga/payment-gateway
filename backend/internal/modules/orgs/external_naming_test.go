package orgs

import "testing"

func TestExternalAccountKindMapping(t *testing.T) {
	tests := []struct {
		stored string
		kind   string
		label  string
	}{
		{stored: AccountKindCreator, kind: "individual", label: "Individual"},
		{stored: AccountKindMerchant, kind: "merchant", label: "Merchant"},
		{stored: "", kind: "", label: "Account"},
	}
	for _, test := range tests {
		if got := ExternalAccountKind(test.stored); got != test.kind {
			t.Errorf("ExternalAccountKind(%q) = %q, want %q", test.stored, got, test.kind)
		}
		if got := ExternalAccountKindLabel(test.stored); got != test.label {
			t.Errorf("ExternalAccountKindLabel(%q) = %q, want %q", test.stored, got, test.label)
		}
	}
}
