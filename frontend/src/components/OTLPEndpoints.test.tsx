// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import type { Settings } from '@/lib/api'
import { OTLPEndpoints } from './OTLPEndpoints'

afterEach(cleanup)

it('uses the public hostname while retaining the OTLP receiver ports and TLS', () => {
  render(
    <OTLPEndpoints
      settings={
        {
          public_url: 'https://telemetry.example.com:8443/spaniel/',
          tls_enabled: false,
          runtime: { otlp_http_port: 4346, otlp_grpc_port: 4345 },
        } as Settings
      }
    />,
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=http\/protobuf/).textContent).toContain(
    'http://telemetry.example.com:4346',
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=grpc/).textContent).toContain(
    'http://telemetry.example.com:4345',
  )
})

it('shows both protocols using live ports and TLS', () => {
  render(
    <OTLPEndpoints
      settings={
        {
          tls_enabled: true,
          runtime: { otlp_http_port: 14318, otlp_grpc_port: 14317 },
        } as Settings
      }
    />,
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=http\/protobuf/).textContent).toContain(
    `https://${window.location.hostname}:14318`,
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=grpc/).textContent).toContain(
    `https://${window.location.hostname}:14317`,
  )
})

it('does not advertise an endpoint for a disabled receiver', () => {
  render(
    <OTLPEndpoints
      settings={
        {
          tls_enabled: false,
          bearer_token_set: true,
          runtime: { otlp_http_port: 0, otlp_grpc_port: 4345 },
        } as Settings
      }
    />,
  )
  expect(screen.getByText('OTLP HTTP/protobuf · disabled')).toBeTruthy()
  expect(screen.queryByText(/OTEL_EXPORTER_OTLP_PROTOCOL=http\/protobuf/)).toBeNull()
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=grpc/).textContent).toContain(
    `http://${window.location.hostname}:4345`,
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_HEADERS/)).toBeTruthy()
})

it('uses independent advertised URLs instead of internal ports', () => {
  render(
    <OTLPEndpoints
      settings={
        {
          public_url: 'https://ui.example.com',
          otlp_http_url: 'https://http.example.com/otlp',
          otlp_grpc_url: 'https://grpc.example.com:443',
          runtime: { otlp_http_port: 4318, otlp_grpc_port: 4317 },
        } as Settings
      }
    />,
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=http\/protobuf/).textContent).toContain(
    'https://http.example.com/otlp',
  )
  expect(screen.getByText(/OTEL_EXPORTER_OTLP_PROTOCOL=grpc/).textContent).toContain(
    'https://grpc.example.com:443',
  )
})
