package telemetry

import "github.com/zfogg/gotel/go/httptrace"

var HTTP = httptrace.Namer{Prefix: "spaniel"}
