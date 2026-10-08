import type { components } from '../openapi'
type APIModels = components['schemas']

type Meta = APIModels['Meta']
export interface Envelope<T> {
  data: T
  meta?: Meta
}

type GeneratedResponse<T> = {
  data?: T
  error?: APIModels['Error']
  response: Response
}

export async function unwrap<T extends Envelope<unknown>>(
  call: Promise<GeneratedResponse<T>>,
): Promise<T & { meta?: Meta }> {
  const { data, error, response } = await call
  if (error || !response.ok) {
    throw new Error(error?.error ?? `${response.status} ${response.statusText}`)
  }
  if (!data) throw new Error(`${response.status} ${response.statusText}`)
  return data
}
