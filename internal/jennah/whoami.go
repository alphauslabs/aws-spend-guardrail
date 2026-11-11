package jennah

import (
	"context"
	"fmt"
	"time"
)

// KeyInfo is what Jennah reports about the API key a client authenticates with.
type KeyInfo struct {
	// Known is false when Jennah answered without the key: an older Jennah, or
	// one that couldn't read the key's record. Treat that as unknown, never as
	// "never expires".
	Known bool
	ID    string
	Label string
	// ExpiresAt is zero for a key that never expires.
	ExpiresAt time.Time
}

// CallingKey asks Jennah to describe the key this client uses. It needs no
// permission beyond the key itself.
func (c *Client) CallingKey(ctx context.Context) (KeyInfo, error) {
	resp, err := c.c.Auth.WhoAmI(ctx)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("whoami: %w", err)
	}
	k := resp.GetApiKey()
	if k == nil {
		return KeyInfo{}, nil
	}
	info := KeyInfo{Known: true, ID: k.GetKeyId(), Label: k.GetLabel()}
	if k.GetExpiresAt() != nil {
		info.ExpiresAt = k.GetExpiresAt().AsTime().UTC()
	}
	return info, nil
}
