package telemetry

import gotelhttp "github.com/zfogg/gotel/go/tracers/http"

var HTTP = gotelhttp.Namer{Prefix: "spaniel"}
