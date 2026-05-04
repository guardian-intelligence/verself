package notifications

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	DomainEventContentType = "application/vnd.verself.domain-event+json;version=1"
	NATSDefaultURL         = "tls://127.0.0.1:4222"
)

type NATSBus struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	logger *slog.Logger
}

func NewNATSBus(ctx context.Context, url string, source *workloadapi.X509Source, logger *slog.Logger) (*NATSBus, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if source == nil {
		return nil, fmt.Errorf("spiffe x509 source is required")
	}
	if strings.TrimSpace(url) == "" {
		url = NATSDefaultURL
	}
	natsID, err := workloadauth.PeerIDForSource(source, workloadauth.ServiceNATS)
	if err != nil {
		return nil, err
	}
	tlsConfig := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(natsID))
	conn, err := nats.Connect(
		url,
		nats.Name(ServiceName),
		nats.Secure(tlsConfig),
		nats.Timeout(3*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(250*time.Millisecond),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.WarnContext(ctx, "nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) {
			logger.InfoContext(ctx, "nats reconnected", "url", conn.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect nats: %w", err)
	}
	js, err := conn.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("open nats jetstream: %w", err)
	}
	bus := &NATSBus{conn: conn, js: js, logger: logger}
	if err := bus.EnsureStream(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return bus, nil
}

func (b *NATSBus) EnsureStream(ctx context.Context) error {
	_, span := tracer.Start(ctx, "notifications.nats.ensure_stream")
	defer span.End()
	span.SetAttributes(attribute.String("messaging.system", "nats"), attribute.String("messaging.destination.name", EventsStreamName))
	config := &nats.StreamConfig{
		Name:       EventsStreamName,
		Subjects:   []string{EventsSubjectPattern},
		Retention:  nats.LimitsPolicy,
		Storage:    nats.FileStorage,
		Replicas:   1,
		Duplicates: time.Hour,
		MaxAge:     7 * 24 * time.Hour,
	}
	info, err := b.js.StreamInfo(EventsStreamName)
	if errors.Is(err, nats.ErrStreamNotFound) {
		if _, err := b.js.AddStream(config); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("add nats stream %s: %w", EventsStreamName, err)
		}
		return nil
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("inspect nats stream %s: %w", EventsStreamName, err)
	}
	if info.Config.Storage != config.Storage || info.Config.Retention != config.Retention {
		return fmt.Errorf("nats stream %s exists with incompatible storage or retention", EventsStreamName)
	}
	return nil
}

func (b *NATSBus) PublishDomainEvent(ctx context.Context, event DomainEvent) error {
	ctx, span := tracer.Start(ctx, "notifications.nats.publish")
	defer span.End()
	event, err := NormalizeDomainEvent(event)
	if err != nil {
		return err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("%w: marshal domain event: %v", ErrInvalidInput, err)
	}
	subject := event.Subject
	if !strings.HasPrefix(subject, "events.") {
		subject = "events." + strings.TrimPrefix(subject, ".")
	}
	msg := &nats.Msg{
		Subject: subject,
		Header:  nats.Header{},
		Data:    body,
	}
	msg.Header.Set("Content-Type", DomainEventContentType)
	msg.Header.Set("Nats-Msg-Id", event.EventID.String())
	if event.Traceparent != "" {
		msg.Header.Set("traceparent", event.Traceparent)
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))
	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination.name", EventsStreamName),
		attribute.String("messaging.destination.subject", subject),
		attribute.String("notification.event_id", event.EventID.String()),
		attribute.String("notification.event_source", event.EventSource),
	)
	if _, err := b.js.PublishMsg(msg, nats.MsgId(event.EventID.String())); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("publish notification domain event: %w", err)
	}
	return nil
}

func (b *NATSBus) RunConsumer(ctx context.Context, svc *Service) error {
	if svc == nil {
		return fmt.Errorf("notifications service is required")
	}
	sub, err := b.js.PullSubscribe(
		EventsSubjectPattern,
		EventsConsumerDurable,
		nats.BindStream(EventsStreamName),
		nats.ManualAck(),
		nats.AckExplicit(),
	)
	if err != nil {
		return fmt.Errorf("subscribe notifications nats consumer: %w", err)
	}
	for ctx.Err() == nil {
		msgs, err := sub.Fetch(32, nats.MaxWait(time.Second))
		if errors.Is(err, nats.ErrTimeout) {
			continue
		}
		if err != nil {
			b.logger.WarnContext(ctx, "notifications nats fetch", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
				continue
			}
		}
		for _, msg := range msgs {
			b.consumeMessage(ctx, svc, msg)
		}
	}
	return ctx.Err()
}

func (b *NATSBus) Close() {
	if b == nil || b.conn == nil {
		return
	}
	_ = b.conn.Drain()
	b.conn.Close()
}

func (b *NATSBus) Ready() error {
	if b == nil || b.conn == nil || !b.conn.IsConnected() {
		return fmt.Errorf("%w: nats is not connected", ErrStoreUnavailable)
	}
	return nil
}

func (b *NATSBus) consumeMessage(ctx context.Context, svc *Service, msg *nats.Msg) {
	if msg == nil {
		return
	}
	parent := otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(msg.Header))
	parent, span := tracer.Start(parent, "notifications.nats.consume")
	defer span.End()
	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination.name", EventsStreamName),
		attribute.String("messaging.message.subject", msg.Subject),
	)
	var event DomainEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid domain event json")
		_ = msg.Term()
		return
	}
	if event.Traceparent != "" {
		parent = propagation.TraceContext{}.Extract(parent, propagation.MapCarrier{"traceparent": event.Traceparent})
	}
	span.SetAttributes(
		attribute.String("notification.event_id", event.EventID.String()),
		attribute.String("notification.event_source", event.EventSource),
		attribute.String("notification.subject", event.Subject),
	)
	accepted, err := svc.AcceptEvent(parent, event)
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			_ = msg.Term()
			return
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		_ = msg.Nak()
		return
	}
	span.SetAttributes(attribute.Bool("notification.accepted", accepted))
	if err := msg.Ack(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func CurrentNATSClientTLSConfig(source *workloadapi.X509Source, expected spiffeid.ID) (*tls.Config, error) {
	if source == nil {
		return nil, fmt.Errorf("spiffe x509 source is required")
	}
	if expected.IsZero() {
		return nil, fmt.Errorf("nats spiffe id is required")
	}
	return tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(expected)), nil
}
