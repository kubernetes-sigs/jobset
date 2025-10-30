package events

import (
	"context"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	"k8s.io/utils/lru"
)

type Throttler struct {
	recorder           record.EventRecorder
	windowSize         time.Duration
	cache              *lru.Cache
	cacheResetInterval time.Duration
	clock              clock.Clock
}

func NewThrottler(recorder record.EventRecorder, windowSize time.Duration, cacheSize int, cacheResetInterval time.Duration, clock clock.Clock) *Throttler {
	return &Throttler{
		recorder:           recorder,
		windowSize:         windowSize,
		cache:              lru.New(cacheSize),
		cacheResetInterval: cacheResetInterval,
		clock:              clock,
	}
}

func (t *Throttler) Run(ctx context.Context) {
	ticker := time.NewTicker(t.cacheResetInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.cache.Clear()

		case <-ctx.Done():
			return
		}
	}
}

type throttlingCacheRecord struct {
	Timestamp  time.Time
	HashObject any
}

func (t *Throttler) RecordThrottledEvent(eventKey string, hashObject any, object runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	v, ok := t.cache.Get(eventKey)
	t.cache.Add(eventKey, throttlingCacheRecord{
		Timestamp:  t.clock.Now(),
		HashObject: hashObject,
	})

	if ok {
		cacheRecord := v.(throttlingCacheRecord)
		if time.Since(cacheRecord.Timestamp) < t.windowSize && reflect.DeepEqual(hashObject, cacheRecord.HashObject) {
			return
		}
	}

	t.recorder.Eventf(object, eventType, reason, messageFmt, args...)
}
