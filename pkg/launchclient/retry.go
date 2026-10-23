package launchclient

import (
	"context"
	"errors"
	"time"
)

// RetryPolicy bounds synchronization polling. Only ENTITLEMENT_PENDING is
// retried; authentication failures, expired rights and transport failures are not.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func (c *Client) CredentialWithRetry(ctx context.Context, product, environment string, policy RetryPolicy) (Credential, error) {
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 5
	}
	if policy.InitialDelay == 0 {
		policy.InitialDelay = time.Second
	}
	if policy.MaxDelay == 0 {
		policy.MaxDelay = 10 * time.Second
	}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 10 || policy.InitialDelay < 0 || policy.MaxDelay < policy.InitialDelay || policy.MaxDelay > time.Minute {
		return Credential{}, errors.New("invalid credential retry policy")
	}
	delay := policy.InitialDelay
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return Credential{}, err
		}
		credential, err := c.Credential(ctx, product, environment)
		var api *APIError
		if err == nil || attempt >= policy.MaxAttempts || !errors.As(err, &api) || api.Code != "ENTITLEMENT_PENDING" {
			return credential, err
		}
		wait := max(delay, api.RetryAfter)
		// Never retry before the server's advertised time. A long Retry-After
		// ends this bounded operation instead of silently shortening the delay.
		if wait > policy.MaxDelay {
			return Credential{}, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Credential{}, ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, policy.MaxDelay)
	}
}
