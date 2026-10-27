package klap

import (
	"context"
	"testing"
)

func TestAppleSyncRejectsUnsupportedOrDisconnectedMode(t *testing.T) {
	for _, service := range []*Service{
		{},
		{Apple: true, AppleReady: func() bool { return false }},
	} {
		// No Core is present: rejected calls must never reach KLAS or EventKit.
		if _, err := service.Sync(context.Background(), nil); err == nil {
			t.Fatal("unavailable Apple sync was accepted")
		}
	}
}
