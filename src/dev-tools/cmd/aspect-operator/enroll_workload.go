package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// cmdEnrollWorkload mints a single-use AppRole bootstrap secret + claims
// a workload-pool slot on behalf of the operator. The output is a shell
// env block that the operator pastes into the new VM's environment;
// the workload's verself-workload-bootstrap binary trades the env vars
// for a 24h Vault token and signs an SSH cert.
//
// Lease and token TTLs are both fixed at the workload AppRole's
// token_max_ttl (24h server-side). Decoupling them was a footgun: an
// operator-supplied --hours below 24 would free the slot in OpenBao KV
// before the workload's cert expired, letting a subsequent enrollment
// claim the still-in-use slot's wg priv key. The lease and the token
// share the same 24h window by construction.
func cmdEnrollWorkload(args []string) error {
	fs := flagSet("enroll-workload")
	slot := fs.Int("slot", -1, "Slot index to claim (default: scan for the first free slot)")
	device := fs.String("device", "", "Workload tag stamped into the cert KeyID (default: random)")
	domain := fs.String("domain", "verself.sh", "Public domain that serves /.well-known/verself-*")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := operatorConfigDir()
	if err != nil {
		return err
	}
	anchors, err := fetchTrustAnchors(*domain, cfg)
	if err != nil {
		return err
	}

	tokenBytes, err := os.ReadFile(vaultTokenPath())
	if err != nil {
		return fmt.Errorf("no cached Vault token: %w (run `aspect operator onboard ...` first)", err)
	}
	bao, err := newBaoClient(
		fmt.Sprintf("https://%s:8200", anchors.Wireguard.HostAddress),
		anchors.OpenBaoCAPath,
		strings.TrimSpace(string(tokenBytes)),
	)
	if err != nil {
		return err
	}

	// 1. Pick a slot. Either honour --slot or scan kv leases for the
	//    first free one. Leases are stored under
	//    kv-workload-pool/data/leases/<n> with shape
	//    {claimed_by, claimed_at, expires_at_unix}; an absent key or
	//    expired lease counts as free.
	chosen, err := claimSlot(bao, *slot)
	if err != nil {
		return err
	}

	// 2. Read the slot's pre-generated wg priv key + address.
	type kvSlotData struct {
		Data struct {
			Data struct {
				WGPrivateKey string `json:"wg_private_key"`
				WGPublicKey  string `json:"wg_public_key"`
				WGAddress    string `json:"wg_address"`
			} `json:"data"`
		} `json:"data"`
	}
	var slotData kvSlotData
	if _, err := bao.do("GET", fmt.Sprintf("/v1/kv-workload-pool/data/slots/%d", chosen), nil, &slotData); err != nil {
		return fmt.Errorf("read slot %d data: %w", chosen, err)
	}

	// 3. Mint the bootstrap secret-id. Single-use, 15-min TTL by role
	//    config; we do not pass a TTL override.
	type secretIDOut struct {
		Data struct {
			SecretID         string `json:"secret_id"`
			SecretIDAccessor string `json:"secret_id_accessor"`
			SecretIDTTL      int    `json:"secret_id_ttl"`
		} `json:"data"`
	}
	var sid secretIDOut
	body := map[string]any{}
	if *device != "" {
		body["metadata"] = fmt.Sprintf(`{"device":%q,"slot":%d}`, *device, chosen)
	}
	if _, err := bao.do("POST", "/v1/auth/approle/role/workload-enrollment/secret-id", body, &sid); err != nil {
		return fmt.Errorf("mint workload bootstrap secret-id: %w", err)
	}

	// 4. Read the role_id once. (Static across redeploys; we read it
	//    from the AppRole API rather than from the host credstore so
	//    this binary doesn't need SSH access to fetch it.)
	type roleIDOut struct {
		Data struct {
			RoleID string `json:"role_id"`
		} `json:"data"`
	}
	var rid roleIDOut
	if _, err := bao.do("GET", "/v1/auth/approle/role/workload-enrollment/role-id", nil, &rid); err != nil {
		return fmt.Errorf("read workload-enrollment role-id: %w", err)
	}

	// 5. Emit env block. Quote everything single — the secret-id is
	//    high-entropy and the wg priv key contains '+', '/', '='.
	tag := *device
	if tag == "" {
		tag = fmt.Sprintf("slot-%d", chosen)
	}
	out := fmt.Sprintf(`# verself workload bootstrap envelope
# slot:    %d
# device:  %s
# expires: %s (secret-id TTL %ds, single-use)
# Paste into the workload VM and run verself-workload-bootstrap.
export VERSELF_DOMAIN=%q
export VERSELF_WG_ENDPOINT=%q
export VERSELF_WG_HOST_ADDRESS=%q
export VERSELF_WG_NETWORK=%q
export VERSELF_WG_SERVER_PUBKEY=%q
export VERSELF_SLOT=%d
export VERSELF_DEVICE=%q
export VERSELF_BOOTSTRAP_ROLE_ID=%q
export VERSELF_BOOTSTRAP_SECRET_ID=%q
export VERSELF_WG_PRIVATE_KEY=%q
export VERSELF_WG_ADDRESS=%q
`,
		chosen, tag,
		time.Now().Add(time.Duration(sid.Data.SecretIDTTL)*time.Second).UTC().Format(time.RFC3339),
		sid.Data.SecretIDTTL,
		*domain,
		fmt.Sprintf("%s:%d", anchors.Wireguard.EndpointHost, anchors.Wireguard.EndpointPort),
		anchors.Wireguard.HostAddress,
		anchors.Wireguard.Network,
		anchors.Wireguard.ServerPubkey,
		chosen, tag,
		rid.Data.RoleID,
		sid.Data.SecretID,
		slotData.Data.Data.WGPrivateKey,
		slotData.Data.Data.WGAddress,
	)
	fmt.Print(out)
	return nil
}

// claimSlot finds a free workload-pool slot and writes a lease. When
// requested is >=0 it claims that exact slot (failing if a non-expired
// lease already exists). Otherwise it scans 0..slot_count-1 and picks
// the first free one. Lease window is hard-coded to 24h to match the
// workload AppRole's token_max_ttl — see the cmdEnrollWorkload doc.
func claimSlot(bao *baoClient, requested int) (int, error) {
	const leaseWindowSeconds = 86400
	type slotsJSON struct {
		Data struct {
			Data struct {
				ClaimedBy     string `json:"claimed_by"`
				ExpiresAtUnix int64  `json:"expires_at_unix"`
			} `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	now := time.Now().Unix()
	expires := now + leaseWindowSeconds
	whoami := os.Getenv("USER")
	if whoami == "" {
		whoami = "unknown"
	}
	host, _ := os.Hostname()
	leaseBody := map[string]any{
		"data": map[string]any{
			"claimed_by":      fmt.Sprintf("%s@%s", whoami, host),
			"claimed_at_unix": now,
			"expires_at_unix": expires,
		},
	}

	if requested >= 0 {
		var existing slotsJSON
		status, err := bao.do("GET", fmt.Sprintf("/v1/kv-workload-pool/data/leases/%d", requested), nil, &existing)
		if err != nil && status != 404 {
			return -1, err
		}
		if status == 200 && existing.Data.Data.ExpiresAtUnix > now {
			return -1, fmt.Errorf("slot %d held by %s until %s; pick another --slot or wait",
				requested,
				existing.Data.Data.ClaimedBy,
				time.Unix(existing.Data.Data.ExpiresAtUnix, 0).UTC().Format(time.RFC3339),
			)
		}
		if _, err := bao.do("POST", fmt.Sprintf("/v1/kv-workload-pool/data/leases/%d", requested), leaseBody, nil); err != nil {
			return -1, err
		}
		return requested, nil
	}

	// Scan slot_count by probing /slots/<n>; the slot with no /leases/<n>
	// (or an expired one) wins.
	for slot := 0; slot < 64; slot++ {
		var slotProbe map[string]any
		status, _ := bao.do("GET", fmt.Sprintf("/v1/kv-workload-pool/data/slots/%d", slot), nil, &slotProbe)
		if status == 404 {
			break // ran past the end of the configured pool
		}
		var existing slotsJSON
		leaseStatus, err := bao.do("GET", fmt.Sprintf("/v1/kv-workload-pool/data/leases/%d", slot), nil, &existing)
		if err != nil && leaseStatus != 404 {
			return -1, err
		}
		if leaseStatus == 200 && existing.Data.Data.ExpiresAtUnix > now {
			continue
		}
		if _, err := bao.do("POST", fmt.Sprintf("/v1/kv-workload-pool/data/leases/%d", slot), leaseBody, nil); err != nil {
			return -1, err
		}
		return slot, nil
	}
	return -1, errors.New("no free workload slot in the pool; bump workloads.pool.slot_count and redeploy")
}
