package config

import (
	"strings"
	"testing"
	"time"
)

func TestHTTPSettingsPreserveLegacyOverrides(t *testing.T) {
	file := settingsFile(t, "RATE_LIMIT_CAPACITY=80\nRATE_LIMIT_REFILL=2.5\nBLOCKING_DB_WORKER_COUNT=4\nBLOCKING_DB_QUEUE_CAPACITY=3\nDB_HEALTH_TIMEOUT=0.25\nHEALTH_RETRY_AFTER=7\nTRUSTED_PROXIES=127.0.0.1,10.0.0.0/8,::1\n")
	s, err := Load(file, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if s.HTTP.Capacity != 80 || s.HTTP.Refill != 2.5 || s.HTTP.Concurrency != 7 || s.HTTP.HealthTimeout != 250*time.Millisecond || s.HTTP.HealthRetryAfter != 7 || len(s.HTTP.TrustedProxies) != 3 {
		t.Fatal(s.HTTP)
	}
	s, err = Load(file, func(k string) (string, bool) { return "9", k == "HEALTH_RETRY_AFTER" })
	if err != nil || s.HTTP.HealthRetryAfter != 9 {
		t.Fatal(s.HTTP, err)
	}
}
func TestHTTPSettingsRejectUnsafeValues(t *testing.T) {
	for _, v := range []string{"RATE_LIMIT_CAPACITY=0", "RATE_LIMIT_REFILL=NaN", "RATE_LIMIT_REFILL=Inf", "BLOCKING_DB_WORKER_COUNT=0", "BLOCKING_DB_QUEUE_CAPACITY=-1", "BLOCKING_DB_WORKER_COUNT=4096\nBLOCKING_DB_QUEUE_CAPACITY=1", "DB_HEALTH_TIMEOUT=0", "DB_HEALTH_TIMEOUT=NaN", "DB_HEALTH_TIMEOUT=11", "HEALTH_RETRY_AFTER=0", "TRUSTED_PROXIES=*", "TRUSTED_PROXIES=proxy.example", "TRUSTED_PROXIES=127.0.0.1,"} {
		if _, err := Load(settingsFile(t, v+"\n"), noEnv); err == nil {
			t.Fatal("accepted", strings.Split(v, "=")[0])
		}
	}
	if err := DefaultHTTPSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerOverrideDoesNotChangeLegacyQueueDefault(t *testing.T) {
	// Python computes both defaults at class definition time; changing only the
	// worker env var must not silently resize the unspecified queue.
	s, err := Load(settingsFile(t, "BLOCKING_DB_WORKER_COUNT=1\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	want := 1 + 2*(DefaultHTTPSettings().Concurrency/3)
	if s.HTTP.Concurrency != want {
		t.Fatalf("admission %d, want %d", s.HTTP.Concurrency, want)
	}
}
