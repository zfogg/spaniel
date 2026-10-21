package telemetry

import "github.com/zfogg/gotel/httptrace"

var HTTP = httptrace.Namer{Prefix: "spaniel"}
