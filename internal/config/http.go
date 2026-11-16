package config

import (
	"fmt"
	"math"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// HTTPSettings carries the existing Python admission/probe settings. The Go
// semaphore bounds running plus queued DB work; it does not spawn worker threads.
type HTTPSettings struct {
	Capacity         int            `json:"rate_capacity"`
	Refill           float64        `json:"rate_refill"`
	Concurrency      int            `json:"db_admission"`
	TrustedProxies   []netip.Prefix `json:"trusted_proxies"`
	HealthTimeout    time.Duration  `json:"-"`
	HealthRetryAfter int            `json:"health_retry_after"`
}

func DefaultHTTPSettings() HTTPSettings {
	workers := max(4, min(32, 4*runtime.NumCPU()))
	return HTTPSettings{Capacity: 60, Refill: 1, Concurrency: 3 * workers,
		HealthTimeout: time.Second, HealthRetryAfter: 5}
}

func (s HTTPSettings) Validate() error {
	if s.Capacity < 1 || s.Capacity > 100000 || s.Refill <= 0 || s.Refill > 100000 || math.IsNaN(s.Refill) || math.IsInf(s.Refill, 0) || s.Concurrency < 1 || s.Concurrency > 4096 || s.HealthTimeout < time.Millisecond || s.HealthTimeout > 10*time.Second || s.HealthRetryAfter < 1 || s.HealthRetryAfter > 86400 {
		return fmt.Errorf("invalid HTTP admission or health settings")
	}
	for _, p := range s.TrustedProxies {
		if !p.IsValid() {
			return fmt.Errorf("invalid trusted proxy prefix")
		}
	}
	return nil
}

func loadHTTP(get func(string, string) string) (HTTPSettings, error) {
	s := DefaultHTTPSettings()
	integer := func(key string, fallback, low, high int) (int, error) {
		v, e := strconv.Atoi(get(key, strconv.Itoa(fallback)))
		if e != nil || v < low || v > high {
			return 0, fmt.Errorf("invalid %s", key)
		}
		return v, nil
	}
	decimal := func(key string, fallback float64, low, high float64) (float64, error) {
		v, e := strconv.ParseFloat(get(key, strconv.FormatFloat(fallback, 'g', -1, 64)), 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < low || v > high {
			return 0, fmt.Errorf("invalid %s", key)
		}
		return v, nil
	}
	var err error
	if s.Capacity, err = integer("RATE_LIMIT_CAPACITY", s.Capacity, 1, 100000); err != nil {
		return s, err
	}
	if s.Refill, err = decimal("RATE_LIMIT_REFILL", 1, 0.000001, 100000); err != nil {
		return s, err
	}
	workers, err := integer("BLOCKING_DB_WORKER_COUNT", s.Concurrency/3, 1, 4096)
	if err != nil {
		return s, err
	}
	queue, err := integer("BLOCKING_DB_QUEUE_CAPACITY", 2*workers, 0, 4096)
	if err != nil {
		return s, err
	}
	s.Concurrency = workers + queue
	seconds, err := decimal("DB_HEALTH_TIMEOUT", 1, 0.001, 10)
	if err != nil {
		return s, err
	}
	s.HealthTimeout = time.Duration(seconds * float64(time.Second))
	if s.HealthRetryAfter, err = integer("HEALTH_RETRY_AFTER", 5, 1, 86400); err != nil {
		return s, err
	}
	raw := strings.TrimSpace(get("TRUSTED_PROXIES", ""))
	if raw != "" {
		for _, v := range strings.Split(raw, ",") {
			v = strings.TrimSpace(v)
			p, e := netip.ParsePrefix(v)
			if e != nil {
				ip, ipErr := netip.ParseAddr(v)
				if ipErr != nil {
					return s, fmt.Errorf("TRUSTED_PROXIES must contain IP addresses or CIDR prefixes")
				}
				ip = ip.Unmap()
				p = netip.PrefixFrom(ip, ip.BitLen())
			}
			s.TrustedProxies = append(s.TrustedProxies, p.Masked())
		}
	}
	return s, s.Validate()
}
