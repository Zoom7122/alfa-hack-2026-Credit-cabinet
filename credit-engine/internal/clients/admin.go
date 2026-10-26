package clients

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

type AdminClient struct{ transport *transport }

func NewAdmin(base string, c *http.Client) (*AdminClient, error) {
	t, err := newTransport(base, c)
	if err != nil {
		return nil, err
	}
	return &AdminClient{transport: t}, nil
}

// NewAdminWithToken authenticates only the administrative event client.
// Redirects remain disabled by the shared transport, including same-host ones.
func NewAdminWithToken(base, token string, c *http.Client) (*AdminClient, error) {
	if !config.ValidServiceToken(token) {
		return nil, fmt.Errorf("admin service token must contain 32..256 visible ASCII characters without spaces")
	}
	client, err := NewAdmin(base, c)
	if err != nil {
		return nil, err
	}
	client.transport.headers = http.Header{"Authorization": []string{"Bearer " + token}}
	return client, nil
}
func (c *AdminClient) Notify(ctx context.Context, event contracts.AdminEvent) (contracts.EventAck, error) {
	var ack contracts.EventAck
	_, data, err := c.transport.exchange(ctx, http.MethodPost, []string{"api", "v1", "credit-events"}, nil, event, http.StatusAccepted, 0)
	if err != nil {
		return ack, err
	}
	if err = decodeObject(data, &ack, []string{"event_id", "accepted"}, nil); err != nil {
		return contracts.EventAck{}, err
	}
	if !ack.Accepted || ack.EventID == "" || ack.EventID != event.EventID {
		return contracts.EventAck{}, invalidResponse("INVALID_EVENT_ACK")
	}
	return ack, nil
}

var _ bank.Notifier = (*AdminClient)(nil)
