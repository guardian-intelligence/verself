package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	verself "github.com/verself/verself-go"
)

func (c CLI) runNotifications(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("notifications command is required")
	}
	switch args[0] {
	case "list", "ls":
		return c.notificationsList(ctx, args[1:])
	case "summary":
		return c.notificationsSummary(ctx, args[1:])
	case "preferences":
		return c.runNotificationPreferences(ctx, args[1:])
	case "read-cursor":
		return c.notificationsReadCursor(ctx, args[1:])
	case "read":
		return c.notificationsRead(ctx, args[1:])
	case "dismiss":
		return c.notificationsDismiss(ctx, args[1:])
	case "clear":
		return c.notificationsClear(ctx, args[1:])
	case "test":
		return c.notificationsTest(ctx, args[1:])
	default:
		return fmt.Errorf("unknown notifications command %q", args[0])
	}
}

func (c CLI) notificationsList(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications list", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	limit := fs.Int64("limit", 50, "page size")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: notifications list [--limit N] [--json]")
	}
	if *limit < 1 || *limit > 100 {
		return errors.New("notifications list requires --limit between 1 and 100")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	page, err := client.Notifications.List(ctx, verself.ListNotificationsOptions{Limit: *limit})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, page)
	}
	for _, notification := range page.Notifications {
		if err := writeNotification(c.out, notification); err != nil {
			return err
		}
	}
	return writeNotificationSummary(c.out, page.Summary)
}

func (c CLI) notificationsSummary(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications summary", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: notifications summary [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	summary, err := client.Notifications.Summary(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, summary)
	}
	return writeNotificationSummary(c.out, summary)
}

func (c CLI) runNotificationPreferences(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("notifications preferences command is required")
	}
	switch args[0] {
	case "set":
		return c.notificationsPreferencesSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown notifications preferences command %q", args[0])
	}
}

func (c CLI) notificationsPreferencesSet(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications preferences set", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	version := fs.Int64("version", -1, "observed preferences version")
	var enabled, webEnabled, emailEnabled, pushEnabled, smsEnabled optionalBoolFlag
	fs.Var(&enabled, "enabled", "global notification delivery toggle")
	fs.Var(&webEnabled, "web-enabled", "web notification delivery toggle")
	fs.Var(&emailEnabled, "email-enabled", "email notification delivery toggle")
	fs.Var(&pushEnabled, "push-enabled", "push notification delivery toggle")
	fs.Var(&smsEnabled, "sms-enabled", "sms notification delivery toggle")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: notifications preferences set --version VERSION --enabled true|false [--json]")
	}
	if *version < 0 || *version > math.MaxInt32 {
		return errors.New("notifications preferences set requires --version between 0 and 2147483647")
	}
	if !enabled.set {
		return errors.New("notifications preferences set requires --enabled")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	summary, err := client.Notifications.PutPreferences(ctx, verself.PutNotificationPreferencesInput{
		Version:        int32(*version),
		Enabled:        enabled.value,
		WebEnabled:     webEnabled.ptr(),
		EmailEnabled:   emailEnabled.ptr(),
		PushEnabled:    pushEnabled.ptr(),
		SMSEnabled:     smsEnabled.ptr(),
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, summary)
	}
	return writeNotificationSummary(c.out, summary)
}

func (c CLI) notificationsReadCursor(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications read-cursor", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: notifications read-cursor <sequence> [--json]")
	}
	sequence, err := parseNotificationSequence(fs.Arg(0))
	if err != nil {
		return err
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	summary, err := client.Notifications.MarkRead(ctx, verself.MarkNotificationsReadInput{
		ReadUpToSequence: sequence,
		IdempotencyKey:   *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, summary)
	}
	return writeNotificationSummary(c.out, summary)
}

func (c CLI) notificationsRead(ctx context.Context, args []string) error {
	return c.notificationIDMutation(ctx, "notifications read", "read", args)
}

func (c CLI) notificationsDismiss(ctx context.Context, args []string) error {
	return c.notificationIDMutation(ctx, "notifications dismiss", "dismiss", args)
}

func (c CLI) notificationIDMutation(ctx context.Context, command, action string, args []string) error {
	fs, serviceFlags := serviceFlagSet(command, c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: %s <notification-id> [--json]", command)
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	input := verself.NotificationIDMutationInput{
		NotificationID: fs.Arg(0),
		IdempotencyKey: *idempotencyKey,
	}
	var summary verself.NotificationSummary
	switch action {
	case "read":
		summary, err = client.Notifications.MarkNotificationRead(ctx, input)
	case "dismiss":
		summary, err = client.Notifications.Dismiss(ctx, input)
	default:
		return fmt.Errorf("unknown notification mutation %q", action)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, summary)
	}
	return writeNotificationSummary(c.out, summary)
}

func (c CLI) notificationsClear(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications clear", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: notifications clear [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	summary, err := client.Notifications.Clear(ctx, verself.NotificationsMutationOptions{IdempotencyKey: *idempotencyKey})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, summary)
	}
	return writeNotificationSummary(c.out, summary)
}

func (c CLI) notificationsTest(ctx context.Context, args []string) error {
	fs, serviceFlags := serviceFlagSet("notifications test", c.err)
	jsonOut := fs.Bool("json", false, "json output")
	idempotencyKey := fs.String("idempotency-key", "", "stable mutation key")
	title := fs.String("title", "", "notification title")
	body := fs.String("body", "", "notification body")
	actionURL := fs.String("action-url", "", "notification action URL")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: notifications test [--title TEXT] [--body TEXT] [--action-url URL] [--json]")
	}
	client, err := c.serviceClient(*serviceFlags)
	if err != nil {
		return err
	}
	accepted, err := client.Notifications.PublishTest(ctx, verself.PublishTestNotificationInput{
		Title:          *title,
		Body:           *body,
		ActionURL:      *actionURL,
		IdempotencyKey: *idempotencyKey,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, accepted)
	}
	return writeNotificationAccepted(c.out, accepted)
}

func parseNotificationSequence(value string) (int64, error) {
	sequence, err := strconv.ParseInt(value, 10, 64)
	if err != nil || sequence < 0 {
		return 0, fmt.Errorf("notification sequence must be a non-negative integer")
	}
	return sequence, nil
}

type optionalBoolFlag struct {
	value bool
	set   bool
}

func (f *optionalBoolFlag) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	f.value = parsed
	f.set = true
	return nil
}

func (f *optionalBoolFlag) String() string {
	if !f.set {
		return ""
	}
	return strconv.FormatBool(f.value)
}

func (f *optionalBoolFlag) ptr() *bool {
	if !f.set {
		return nil
	}
	value := f.value
	return &value
}

func writeNotification(w io.Writer, notification verself.Notification) error {
	return writef(w, "notification\t%s\t%s\t%s\t%d\t%s\n", notification.NotificationID, notification.Priority, notification.Kind, notification.RecipientSequence, notification.Title)
}

func writeNotificationSummary(w io.Writer, summary verself.NotificationSummary) error {
	return writef(w, "summary\tunread_count=%d\tlatest_sequence=%d\tread_up_to_sequence=%d\tpreferences_enabled=%t\n", summary.UnreadCount, summary.LatestSequence, summary.ReadUpToSequence, summary.Preferences.Enabled)
}

func writeNotificationAccepted(w io.Writer, accepted verself.NotificationAccepted) error {
	if accepted.Traceparent == "" {
		return writef(w, "accepted\t%s\n", accepted.EventID)
	}
	return writef(w, "accepted\t%s\t%s\n", accepted.EventID, accepted.Traceparent)
}
