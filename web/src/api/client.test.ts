import { describe, test, expect } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { api, ApiError } from './client'

describe('request error body', () => {
  test('populates ApiError.body with the parsed JSON error payload', async () => {
    const payload = {
      error: 'service_choice_required',
      warehouse_id: 'wh-1',
      services: [{ connector_id: 'c-1', name: 'CH RO' }],
    }
    server.use(
      http.get('/api/v1/execute-error-probe', () =>
        HttpResponse.json(payload, { status: 409 }),
      ),
    )

    await expect(api.get('/api/v1/execute-error-probe')).rejects.toMatchObject({
      status: 409,
      message: 'service_choice_required',
      body: payload,
    })
  })

  test('keeps the status text message when the body is not JSON', async () => {
    server.use(
      http.get('/api/v1/execute-error-probe-text', () =>
        new HttpResponse('boom', { status: 500 }),
      ),
    )

    try {
      await api.get('/api/v1/execute-error-probe-text')
      throw new Error('expected the request to reject')
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError)
      const apiErr = err as ApiError
      expect(apiErr.status).toBe(500)
      expect(apiErr.body).toMatchObject({ error: expect.any(String) })
    }
  })
})
