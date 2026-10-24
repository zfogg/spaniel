package api

import (
	"github.com/spf13/viper"
	"sync/atomic"
	"time"
	_ "time/tzdata"
)

type notificationPolicy struct {
	enabled    bool
	start, end string
	location   *time.Location
	cooldown   int64
}

var notifications atomic.Pointer[notificationPolicy]

func ConfigureNotificationPolicy(v *viper.Viper) {
	zone, e := time.LoadLocation(v.GetString("notification_timezone"))
	if e != nil {
		zone = time.UTC
	}
	notifications.Store(&notificationPolicy{v.GetBool("notification_quiet_enabled"), v.GetString("notification_quiet_start"), v.GetString("notification_quiet_end"), zone, int64(v.GetInt("notification_cooldown_seconds")) * int64(time.Second)})
}
func notificationQuiet(now time.Time) bool {
	p := notifications.Load()
	if p == nil || !p.enabled {
		return false
	}
	hm := now.In(p.location).Format("15:04")
	if p.start == p.end {
		return true
	}
	if p.start < p.end {
		return hm >= p.start && hm < p.end
	}
	return hm >= p.start || hm < p.end
}
