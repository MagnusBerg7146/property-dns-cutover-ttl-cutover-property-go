package cutover

import (
	"testing"
)

func TestAssessCutover(t *testing.T) {
	tests := []struct {
		name     string
		input    Request
		ready    bool
		blockers int
	}{
		{
			name: "property data is ready",
			input: Request{TTL: 120,
				MaintenanceRequests: []MaintenanceRequest{{ID: "mr-42", Priority: "emergency", Status: "closed"}},
				TenantDocuments:     []TenantDocument{{ID: "lease-7", Archived: true}},
				InspectionReminders: []InspectionReminder{{ID: "inspect-9", Scheduled: true}},
			},
			ready: true,
		},
		{
			name: "open emergency blocks cutover",
			input: Request{TTL: 120,
				MaintenanceRequests: []MaintenanceRequest{{ID: "mr-43", Priority: "emergency", Status: "assigned"}},
				TenantDocuments:     []TenantDocument{{ID: "lease-7", Archived: true}},
				InspectionReminders: []InspectionReminder{{ID: "inspect-9", Scheduled: true}},
			},
			blockers: 1,
		},
		{
			name: "pipeline gaps are all reported",
			input: Request{TTL: 900,
				TenantDocuments:     []TenantDocument{{ID: "lease-8", Archived: false}},
				InspectionReminders: []InspectionReminder{{ID: "inspect-10", Scheduled: false}},
			},
			blockers: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Assess(tt.input)
			if got.Ready != tt.ready || len(got.Blockers) != tt.blockers {
				t.Fatalf("Assess() = ready %v, %d blockers; want ready %v, %d blockers", got.Ready, len(got.Blockers), tt.ready, tt.blockers)
			}
		})
	}
}
