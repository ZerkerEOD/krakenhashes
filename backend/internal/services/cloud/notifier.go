package cloud

import (
	"context"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

/*
 * adminDispatcher is the subset of NotificationDispatcher this package needs.
 *
 * Declared here rather than importing the services package to avoid an import
 * cycle: services already imports this package for the cloud service wiring.
 */
type adminDispatcher interface {
	DispatchToAdmins(ctx context.Context, params models.NotificationDispatchParams) error
}

/*
 * DispatchNotifier routes cloud alerts to every admin.
 *
 * Before this existed the reaper was constructed with a nil notifier, which
 * made the teardown-failure escalation dead code — the one alert whose entire
 * purpose is "automation has lost control of something that is still billing"
 * reached nobody, and the notification enum values added for it were unused.
 */
type DispatchNotifier struct {
	dispatcher adminDispatcher
}

// NewDispatchNotifier creates a notifier. A nil dispatcher is tolerated and
// degrades to logging, so a partially-wired backend still records the alert.
func NewDispatchNotifier(dispatcher adminDispatcher) *DispatchNotifier {
	return &DispatchNotifier{dispatcher: dispatcher}
}

/*
 * CloudTeardownFailed pages admins about an instance that will not die.
 *
 * The raw provider instance ID is included deliberately: at this point the
 * operator needs to go to the provider console and kill it by hand, and
 * hunting for the ID costs money by the minute.
 */
func (n *DispatchNotifier) CloudTeardownFailed(ctx context.Context, inst *models.CloudInstance, attempts int, cause error) {
	msg := fmt.Sprintf(
		"Failed to destroy cloud instance %s after %d attempts. It is STILL BILLING at %d cents/hour. "+
			"Provider instance ID: %s. Destroy it from the provider console now.",
		inst.Label, attempts, inst.HourlyRateCents, providerIDOrUnknown(inst))
	if cause != nil {
		msg += " Last error: " + cause.Error()
	}

	debug.Error("CLOUD TEARDOWN FAILURE: %s", msg)

	if n.dispatcher == nil {
		return
	}
	if err := n.dispatcher.DispatchToAdmins(ctx, models.NotificationDispatchParams{
		Type:       models.NotificationTypeCloudTeardownFailed,
		Title:      "Cloud instance teardown failed — still billing",
		Message:    msg,
		SourceType: "cloud_instance",
		SourceID:   inst.ID.String(),
		Data: map[string]interface{}{
			"label":                inst.Label,
			"provider_instance_id": inst.ProviderInstanceID,
			"attempts":             attempts,
			"hourly_rate_cents":    inst.HourlyRateCents,
		},
	}); err != nil {
		debug.Error("Failed to dispatch cloud teardown alert: %v", err)
	}
}

// CloudBudgetThreshold tells admins a client has crossed a spend threshold.
func (n *DispatchNotifier) CloudBudgetThreshold(ctx context.Context, clientID uuid.UUID, action BudgetAction, reason string) {
	debug.Info("Cloud budget threshold for client %s: %s (%s)", clientID, action, reason)

	if n.dispatcher == nil {
		return
	}
	if err := n.dispatcher.DispatchToAdmins(ctx, models.NotificationDispatchParams{
		Type:       models.NotificationTypeCloudBudgetThreshold,
		Title:      "Cloud spend threshold reached",
		Message:    fmt.Sprintf("Client %s: %s. %s", clientID, action, reason),
		SourceType: "client",
		SourceID:   clientID.String(),
		Data: map[string]interface{}{
			"client_id": clientID.String(),
			"action":    action.String(),
			"reason":    reason,
		},
	}); err != nil {
		debug.Error("Failed to dispatch cloud budget alert: %v", err)
	}
}

func providerIDOrUnknown(inst *models.CloudInstance) string {
	if inst.ProviderInstanceID == "" {
		return "(never recorded — reconcile by label " + inst.Label + ")"
	}
	return inst.ProviderInstanceID
}
