package main

import (
	"github.com/spf13/viper"
	"testing"
)

func TestNotificationTimezoneDefault(t *testing.T) {
	v := viper.New()
	initViper(v)
	if got := v.GetString("notification_timezone"); got != "Local" {
		t.Fatalf("timezone=%q, want Local", got)
	}
	v.Set("notification_timezone", "America/New_York")
	initViper(v)
	if got := v.GetString("notification_timezone"); got != "America/New_York" {
		t.Fatalf("explicit timezone overwritten: %q", got)
	}
}
