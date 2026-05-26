// Sample trace data — a checkout request with an N+1 in pricing.
// Used by every screen so the variants compare apples-to-apples.

const TRACE = {
  id: '9ef357237e8c74950f711b77779d19eb',
  root: 'POST /api/checkout',
  durationMs: 612,
  spanCount: 24,
  serviceCount: 6,
  status: 'OK',
  warnings: 3,
  startedAt: '14:22:08.421',
};

// Each span: { name, svc, depth, start (ms from trace start), dur (ms),
//   kind, attrs, tag (optional badge: 'n+1', 'lint', 'slow'), errored }
const SPANS = [
  { id:'s1',  name:'POST /api/checkout',           svc:'api-gateway',       depth:0, start:0,   dur:612, kind:'server' },
  { id:'s2',  name:'middleware.auth',              svc:'api-gateway',       depth:1, start:2,   dur:8,   kind:'internal' },
  { id:'s3',  name:'rpc.cart.GetCart',             svc:'api-gateway',       depth:1, start:12,  dur:124, kind:'client' },
  { id:'s4',  name:'cart.GetCart',                 svc:'cart-service',      depth:2, start:14,  dur:118, kind:'server' },
  { id:'s5',  name:'SELECT * FROM carts',          svc:'postgres',          depth:3, start:18,  dur:12,  kind:'db' },
  { id:'s6',  name:'rpc.pricing.Quote',            svc:'cart-service',      depth:3, start:32,  dur:84,  kind:'client' },
  { id:'s7',  name:'pricing.Quote',                svc:'pricing-service',   depth:4, start:34,  dur:80,  kind:'server', tag:'n+1' },
  { id:'s8',  name:'cache.get prices',             svc:'redis',             depth:5, start:36,  dur:1,   kind:'cache' },
  { id:'s9',  name:'SELECT price WHERE sku=?',     svc:'postgres',          depth:5, start:40,  dur:8,   kind:'db' },
  { id:'s10', name:'SELECT price WHERE sku=?',     svc:'postgres',          depth:5, start:50,  dur:7,   kind:'db' },
  { id:'s11', name:'SELECT price WHERE sku=?',     svc:'postgres',          depth:5, start:60,  dur:8,   kind:'db' },
  { id:'s12', name:'SELECT price WHERE sku=?',     svc:'postgres',          depth:5, start:71,  dur:9,   kind:'db' },
  { id:'s13', name:'SELECT price WHERE sku=?',     svc:'postgres',          depth:5, start:82,  dur:8,   kind:'db' },
  { id:'s14', name:'rpc.inventory.Reserve',        svc:'api-gateway',       depth:1, start:140, dur:96,  kind:'client' },
  { id:'s15', name:'inventory.Reserve',            svc:'inventory-service', depth:2, start:142, dur:92,  kind:'server', tag:'lint' },
  { id:'s16', name:'SELECT … FROM stock',          svc:'postgres',          depth:3, start:148, dur:22,  kind:'db' },
  { id:'s17', name:'UPDATE stock SET reserved=…',  svc:'postgres',          depth:3, start:174, dur:46,  kind:'db' },
  { id:'s18', name:'rpc.payment.Charge',           svc:'api-gateway',       depth:1, start:240, dur:312, kind:'client', tag:'slow' },
  { id:'s19', name:'payment.Charge',               svc:'payment-service',   depth:2, start:242, dur:308, kind:'server' },
  { id:'s20', name:'http.stripe POST /v1/charges', svc:'stripe',            depth:3, start:248, dur:287, kind:'client', tag:'slow' },
  { id:'s21', name:'INSERT INTO ledger',           svc:'postgres',          depth:3, start:540, dur:8,   kind:'db' },
  { id:'s22', name:'kafka.publish order.created',  svc:'api-gateway',       depth:1, start:556, dur:54,  kind:'producer' },
];

// Lint warnings emitted by the semconv linter.
const LINTS = [
  { sev:'error', code:'SEMCONV.HTTP.METHOD_MISSING', span:'rpc.inventory.Reserve',
    msg:'span kind=server is missing required attribute http.request.method',
    fix:'add http.request.method="POST" via OTel instrumentation' },
  { sev:'warn',  code:'SEMCONV.DB.SYSTEM_MISSING', span:'SELECT price WHERE sku=?',
    msg:'db span missing db.system — set to "postgresql"', fix:'tracer.SetAttribute("db.system","postgresql")' },
  { sev:'warn',  code:'PERF.N+1', span:'pricing.Quote',
    msg:'6 db spans share fingerprint "SELECT price WHERE sku=?" within 50ms', fix:'batch with WHERE sku IN (?)' },
];

// Service map edges + call counts (from spans above).
const SERVICES = [
  { id:'api-gateway',       x:140, y:60,  calls:1 },
  { id:'cart-service',      x:360, y:60,  calls:1 },
  { id:'pricing-service',   x:580, y:30,  calls:1 },
  { id:'inventory-service', x:580, y:120, calls:1 },
  { id:'payment-service',   x:140, y:230, calls:1 },
  { id:'stripe',            x:360, y:230, calls:1 },
  { id:'postgres',          x:580, y:210, calls:9 },
  { id:'redis',             x:780, y:30,  calls:1 },
];
const EDGES = [
  ['api-gateway','cart-service',1,124],
  ['cart-service','pricing-service',1,84],
  ['pricing-service','redis',1,1],
  ['pricing-service','postgres',5,40],
  ['api-gateway','inventory-service',1,96],
  ['inventory-service','postgres',2,68],
  ['api-gateway','payment-service',1,312],
  ['payment-service','stripe',1,287],
  ['payment-service','postgres',1,8],
  ['cart-service','postgres',1,12],
];

// Recent trace list
const TRACES = [
  { id:'9ef3…d19eb', op:'POST /api/checkout',     dur:612, spans:24, t:'just now',  status:'ok',    tag:'n+1' },
  { id:'7b2a…84c19', op:'GET  /api/products',     dur:78,  spans:8,  t:'4s ago',    status:'ok' },
  { id:'52ff…91a02', op:'POST /api/login',        dur:142, spans:6,  t:'12s ago',   status:'ok' },
  { id:'a18c…2204e', op:'GET  /api/cart',         dur:34,  spans:4,  t:'18s ago',   status:'ok' },
  { id:'c041…77b3d', op:'POST /api/webhook',      dur:1840,spans:31, t:'1m ago',    status:'error', tag:'slow' },
  { id:'4ee2…0c889', op:'GET  /api/products/:id', dur:62,  spans:7,  t:'2m ago',    status:'ok' },
  { id:'88d1…5f24b', op:'POST /api/checkout',     dur:498, spans:22, t:'3m ago',    status:'ok',    tag:'baseline' },
];

window.SPANIEL = { TRACE, SPANS, LINTS, SERVICES, EDGES, TRACES };
