package agentapi

import (
	"context"
	"sync"
	"testing"
)

func TestMySQLConcurrentRefreshReplayManyRounds(t *testing.T) {
	s := identityService(t, identityStore(t), "")
	ctx := context.Background()
	name := "refresh-race"
	current, err := s.Enroll(ctx, enrollment(t, s, &name, 0), name, nil)
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		errors := make(chan error, 4)
		tokens := make(chan TokenData, 4)
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func(raw string) {
				defer wg.Done()
				<-start
				v, e := s.Rotate(ctx, raw)
				errors <- e
				if e == nil {
					tokens <- v
				}
			}(current.RefreshToken)
		}
		close(start)
		wg.Wait()
		close(errors)
		close(tokens)
		successes := 0
		for e := range errors {
			if e == nil {
				successes++
			} else {
				requireCode(t, e, 401, "invalid_token")
			}
		}
		if successes != 1 {
			t.Fatal("same refresh token committed more than once", round, successes)
		}
		_, err = s.VerifyAccess(ctx, current.AccessToken)
		requireCode(t, err, 403, "device_revoked")
		current = <-tokens
		identity, e := s.VerifyAccess(ctx, current.AccessToken)
		if e != nil {
			t.Fatal(e)
		}
		var live int
		if e = s.db.DB.QueryRow("SELECT COUNT(*) FROM agent_device_credential WHERE device_id=? AND revoked_at IS NULL", identity.DeviceID).Scan(&live); e != nil || live != 1 {
			t.Fatal("multiple live credentials", live, e)
		}
	}
}
