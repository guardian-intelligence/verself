package namespaceadmin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Spec struct {
	Name      string
	Retention time.Duration
}

func Ensure(ctx context.Context, namespaceClient client.NamespaceClient, specs []Spec) error {
	for _, spec := range specs {
		if err := ensureOne(ctx, namespaceClient, spec); err != nil {
			return err
		}
	}
	return nil
}

func ensureOne(ctx context.Context, namespaceClient client.NamespaceClient, spec Spec) error {
	_, err := namespaceClient.Describe(ctx, spec.Name)
	if err == nil {
		return nil
	}
	var notFound *serviceerror.NamespaceNotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe namespace %s: %w", spec.Name, err)
	}
	if err := namespaceClient.Register(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        spec.Name,
		WorkflowExecutionRetentionPeriod: durationpb.New(spec.Retention),
	}); err != nil {
		var alreadyExists *serviceerror.NamespaceAlreadyExists
		if errors.As(err, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("register namespace %s: %w", spec.Name, err)
	}
	return nil
}
