//go:build integration

package billing

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestReserveSettleAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(7_200_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-phase2")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 40)
	grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 80)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	jobID := JobID(time.Now().UTC().UnixNano() % 1_000_000_000)
	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-phase2",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID), 10),
	})
	if err != nil {
		t.Fatalf("reserve live host: %v", err)
	}
	if got, want := len(reservation.GrantLegs), 2; got != want {
		t.Fatalf("expected %d grant legs, got %d", want, got)
	}

	if err := env.client.Settle(ctx, &reservation, 45); err != nil {
		t.Fatalf("settle live host: %v", err)
	}

	requireGrantBalance(t, env.tbClient, grantA.grantID, 0, 0, 40)
	requireGrantBalance(t, env.tbClient, grantB.grantID, 75, 0, 5)
	t.Logf(
		"verified live reserve+settle org_id=%d product_id=%s grant_a=%x grant_b=%x",
		orgID,
		productID,
		grantA.grantID,
		grantB.grantID,
	)

	var grantRows int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*)
		FROM credit_grants
		WHERE org_id = $1
		  AND product_id = $2
	`, fmt.Sprintf("%d", orgID), productID).Scan(&grantRows); err != nil {
		t.Fatalf("query live grant rows: %v", err)
	}
	if grantRows != 2 {
		t.Fatalf("expected 2 live grant rows, got %d", grantRows)
	}
}
