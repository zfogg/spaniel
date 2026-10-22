package linter

import (
	"fmt"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestHTTPRulesRespectProtocolAndOTLPKinds(t *testing.T) {
	for _, rule := range []string{"http.missing_method", "http.missing_status_code"} {
		for _, s := range []*storage.Span{
			buildSpan(3, "ratio-worker", map[string]string{"db.system.name": "postgresql", "db.operation.name": "INSERT", "db.query.text": "INSERT INTO reference_points VALUES (?)"}),
			buildSpan(3, "cache", map[string]string{"db.system.name": "redis"}),
			buildSpan(4, "producer", map[string]string{"messaging.system": "kafka"}),
		} {
			if _, fired := findRule(rule).Check(s); fired {
				t.Fatalf("%s misidentified non-HTTP span %+v", rule, s)
			}
		}
		if _, fired := findRule(rule).Check(buildSpan(2, "http-server", map[string]string{"url.path": "/health"})); !fired {
			t.Fatalf("%s missed real OTLP HTTP server", rule)
		}
	}
}

func TestGotelConventionRules(t *testing.T) {
	tests := []struct {
		rule  string
		span  *storage.Span
		fires bool
	}{
		{"span.missing_application_prefix", namedSpan("ratio-api", "auth.verify", 1, nil), true},
		{"span.missing_application_prefix", namedSpan("ratio-api", "ratio.auth.verify", 1, nil), false},
		{"span.high_cardinality_path", namedSpan("ratio-api", "ratio.http.server.GET /orders/42", 2, map[string]string{"http.request.method": "GET", "url.path": "/orders/42"}), true},
		{"http.client.missing_destination", namedSpan("ratio-api", "ratio.http.client.GET api.example/orders", 3, map[string]string{"http.request.method": "GET", "url.full": "https://api.example/orders"}), true},
		{"http.server.raw_url_name", namedSpan("ratio-api", "ratio.http.server.GET https://ratio.test/orders?id=1", 2, map[string]string{"http.request.method": "GET"}), true},
		{"redis.nonstandard_name", namedSpan("stonkpile-worker", "sscan", 3, map[string]string{"db.system.name": "redis"}), true},
		{"redis.nonstandard_name", namedSpan("stonkpile-worker", "stonkpile.storage.redis.sscan", 3, map[string]string{"db.system.name": "redis"}), false},
		{"temporal.nonstandard_name", namedSpan("ratio-worker", "RunActivity:Refresh", 1, map[string]string{"temporalActivityType": "Refresh"}), true},
		{"temporal.nonstandard_name", namedSpan("ratio-worker", "ratio.temporal.activity.Refresh", 1, map[string]string{"temporalActivityType": "Refresh"}), false},
	}
	for _, tc := range tests {
		if _, got := findRule(tc.rule).Check(tc.span); got != tc.fires {
			t.Errorf("%s(%s) fired=%v want %v", tc.rule, tc.span.Name, got, tc.fires)
		}
	}
}

func namedSpan(service, name string, kind int, attrs map[string]string) *storage.Span {
	s := buildSpan(kind, service, attrs)
	s.Name = name
	return s
}

// buildSpan creates a minimal *storage.Span for testing lint rules.
func buildSpan(kind int, serviceName string, attrs map[string]string) *storage.Span {
	attrsJSON := "{"
	first := true
	for k, v := range attrs {
		if !first {
			attrsJSON += ","
		}
		attrsJSON += fmt.Sprintf(`%q:%q`, k, v)
		first = false
	}
	attrsJSON += "}"
	return &storage.Span{
		Kind:        kind,
		ServiceName: serviceName,
		Attributes:  attrsJSON,
		StartNs:     1000,
		EndNs:       2000,
	}
}

// findRule returns the LintRule with the given ID, or panics.
func findRule(id string) LintRule {
	for _, r := range rules {
		if r.ID == id {
			return r
		}
	}
	panic("rule not found: " + id)
}

// --- http.missing_method ---

func TestHTTPMissingMethod_ClientNoAttrs_Fires(t *testing.T) {
	rule := findRule("http.missing_method")
	s := buildSpan(3, "svc", map[string]string{"url.full": "https://example.test/"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire for kind=3 with no http method attr")
	}
}

func TestHTTPMissingMethod_ServerNoAttrs_Fires(t *testing.T) {
	rule := findRule("http.missing_method")
	s := buildSpan(2, "svc", map[string]string{"url.path": "/example"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire for kind=4 with no http method attr")
	}
}

func TestHTTPMissingMethod_Internal_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_method")
	s := buildSpan(1, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire for kind=1 (internal)")
	}
}

func TestHTTPMissingMethod_WithAttr_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_method")
	s := buildSpan(3, "svc", map[string]string{"http.request.method": "GET"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when http.request.method is present")
	}
}

func TestHTTPMissingMethod_WithLegacyAttr_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_method")
	s := buildSpan(3, "svc", map[string]string{"http.method": "POST"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when http.method is present")
	}
}

// --- http.missing_status_code ---

func TestHTTPMissingStatusCode_ClientNoAttrs_Fires(t *testing.T) {
	rule := findRule("http.missing_status_code")
	s := buildSpan(3, "svc", map[string]string{"url.full": "https://example.test/"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire for kind=3 with no status code attr")
	}
}

func TestHTTPMissingStatusCode_Internal_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_status_code")
	s := buildSpan(1, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire for kind=1 (internal)")
	}
}

func TestHTTPMissingStatusCode_WithAttr_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_status_code")
	s := buildSpan(3, "svc", map[string]string{"http.response.status_code": "200"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when http.response.status_code is present")
	}
}

func TestHTTPMissingStatusCode_WithLegacyAttr_DoesNotFire(t *testing.T) {
	rule := findRule("http.missing_status_code")
	s := buildSpan(3, "svc", map[string]string{"http.status_code": "200"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when http.status_code is present")
	}
}

// --- db.missing_system ---

func TestDBMissingSystem_WithStatement_Fires(t *testing.T) {
	rule := findRule("db.missing_system")
	s := buildSpan(0, "svc", map[string]string{"db.statement": "SELECT 1"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when db.statement present but db.system absent")
	}
}

func TestDBMissingSystem_WithOperation_Fires(t *testing.T) {
	rule := findRule("db.missing_system")
	s := buildSpan(0, "svc", map[string]string{"db.operation": "query"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when db.operation present but db.system absent")
	}
}

func TestDBMissingSystem_NoDBAttrs_DoesNotFire(t *testing.T) {
	rule := findRule("db.missing_system")
	s := buildSpan(0, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when no db attrs present")
	}
}

func TestDBMissingSystem_WithSystem_DoesNotFire(t *testing.T) {
	rule := findRule("db.missing_system")
	s := buildSpan(0, "svc", map[string]string{"db.statement": "SELECT 1", "db.system": "postgresql"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when db.system is present")
	}
}

func TestDBCurrentSemanticConventionsDoNotFire(t *testing.T) {
	missingSystem := findRule("db.missing_system")
	missingText := findRule("db.missing_statement")
	s := buildSpan(0, "svc", map[string]string{
		"db.system.name":    "postgresql",
		"db.operation.name": "SELECT",
		"db.query.text":     "SELECT ?",
	})
	if _, fired := missingSystem.Check(s); fired {
		t.Error("db.system.name was not recognized")
	}
	if _, fired := missingText.Check(s); fired {
		t.Error("db.query.text was not recognized")
	}
}

// --- db.missing_statement ---

func TestDBMissingStatement_WithSystemNoStatement_Fires(t *testing.T) {
	rule := findRule("db.missing_statement")
	s := buildSpan(0, "svc", map[string]string{"db.system": "mysql"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when db.system present but db.statement absent")
	}
}

func TestDBMissingStatement_RedisOperationWithoutQueryText_DoesNotFire(t *testing.T) {
	rule := findRule("db.missing_statement")
	for _, systemAttr := range []string{"db.system.name", "db.system"} {
		s := buildSpan(3, "ratio-worker", map[string]string{
			systemAttr:         "redis",
			"db.operation.name": "PUBLISH",
		})
		if _, fired := rule.Check(s); fired {
			t.Errorf("expected rule NOT to fire for Redis span using %s", systemAttr)
		}
	}
}

func TestDBMissingStatement_NoSystem_DoesNotFire(t *testing.T) {
	rule := findRule("db.missing_statement")
	s := buildSpan(0, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when db.system is absent")
	}
}

func TestDBMissingStatement_WithBoth_DoesNotFire(t *testing.T) {
	rule := findRule("db.missing_statement")
	s := buildSpan(0, "svc", map[string]string{"db.system": "mysql", "db.statement": "SELECT 1"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when both db.system and db.statement are present")
	}
}

// --- unknown_service ---

func TestUnknownService_UnknownServiceName_Fires(t *testing.T) {
	rule := findRule("unknown_service")
	s := buildSpan(0, "unknown_service", map[string]string{})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire for service_name='unknown_service'")
	}
}

func TestUnknownService_EmptyServiceName_Fires(t *testing.T) {
	rule := findRule("unknown_service")
	s := buildSpan(0, "", map[string]string{})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire for empty service_name")
	}
}

func TestUnknownService_NormalName_DoesNotFire(t *testing.T) {
	rule := findRule("unknown_service")
	s := buildSpan(0, "my-api", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire for normal service_name")
	}
}

// --- zero_duration ---

func TestZeroDuration_StartEqualsEnd_Fires(t *testing.T) {
	rule := findRule("zero_duration")
	s := buildSpan(0, "svc", map[string]string{})
	s.StartNs = 1000
	s.EndNs = 1000
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when start_ns == end_ns > 0")
	}
}

func TestZeroDuration_StartLessThanEnd_DoesNotFire(t *testing.T) {
	rule := findRule("zero_duration")
	s := buildSpan(0, "svc", map[string]string{})
	s.StartNs = 1000
	s.EndNs = 2000
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when start_ns < end_ns")
	}
}

func TestZeroDuration_BothZero_DoesNotFire(t *testing.T) {
	rule := findRule("zero_duration")
	s := buildSpan(0, "svc", map[string]string{})
	s.StartNs = 0
	s.EndNs = 0
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when both start_ns and end_ns are zero")
	}
}

// --- rpc.missing_system ---

func TestRPCMissingSystem_WithRPCService_Fires(t *testing.T) {
	rule := findRule("rpc.missing_system")
	s := buildSpan(0, "svc", map[string]string{"rpc.service": "MyService"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when rpc.service present but rpc.system absent")
	}
}

func TestRPCMissingSystem_WithRPCMethod_Fires(t *testing.T) {
	rule := findRule("rpc.missing_system")
	s := buildSpan(0, "svc", map[string]string{"rpc.method": "DoThing"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when rpc.method present but rpc.system absent")
	}
}

func TestRPCMissingSystem_NoRPCAttrs_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_system")
	s := buildSpan(0, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when no rpc attrs present")
	}
}

func TestRPCMissingSystem_WithSystem_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_system")
	s := buildSpan(0, "svc", map[string]string{"rpc.service": "MyService", "rpc.system": "grpc"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when rpc.system is present")
	}
}

// --- rpc.missing_method ---

func TestRPCMissingMethod_WithSystemNoMethod_Fires(t *testing.T) {
	rule := findRule("rpc.missing_method")
	s := buildSpan(0, "svc", map[string]string{"rpc.system": "grpc"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when rpc.system present but rpc.method absent")
	}
}

func TestRPCMissingMethod_NoSystem_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_method")
	s := buildSpan(0, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when rpc.system is absent")
	}
}

func TestRPCMissingMethod_WithBoth_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_method")
	s := buildSpan(0, "svc", map[string]string{"rpc.system": "grpc", "rpc.method": "SayHello"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when both rpc.system and rpc.method are present")
	}
}

// --- rpc.missing_service ---

func TestRPCMissingService_WithSystemNoService_Fires(t *testing.T) {
	rule := findRule("rpc.missing_service")
	s := buildSpan(0, "svc", map[string]string{"rpc.system": "grpc"})
	_, fired := rule.Check(s)
	if !fired {
		t.Errorf("expected rule to fire when rpc.system present but rpc.service absent")
	}
}

func TestRPCMissingService_NoSystem_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_service")
	s := buildSpan(0, "svc", map[string]string{})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when rpc.system is absent")
	}
}

func TestRPCMissingService_WithBoth_DoesNotFire(t *testing.T) {
	rule := findRule("rpc.missing_service")
	s := buildSpan(0, "svc", map[string]string{"rpc.system": "grpc", "rpc.service": "helloworld.Greeter"})
	_, fired := rule.Check(s)
	if fired {
		t.Errorf("expected rule NOT to fire when both rpc.system and rpc.service are present")
	}
}
